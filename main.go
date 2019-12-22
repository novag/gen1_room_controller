package main

import (
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "io/ioutil"
    "os"
    "os/exec"
    "os/signal"
    "syscall"

    "github.com/novag/gen1_room_controller/miio"
    "github.com/eclipse/paho.mqtt.golang"
)

const (
    miioTokenPath = "/mnt/data/miio/device.token"
    rockroboBasePath = "/mnt/data/rockrobo/"
    roomControllerBasePath = "/mnt/data/room_controller/"
    fullMapPath = roomControllerBasePath + "full/"
    sshPrivateKeyPath = "/root/.ssh/id_ed25519"
    sshPublicKeyPath = "/root/.ssh/id_ed25519.pub"
    sshKnownHostsPath = "/root/.ssh/known_hosts"
)

type Coordinates struct {
    X int
    Y int
}

type RemoteHost struct {
    Address     string
    Port        string
    FetchKey    bool
}

func getMiioToken() (string, error) {
    data, err := ioutil.ReadFile(miioTokenPath)
    if err != nil {
        return "", err
    }

    data = data[:16]

    return hex.EncodeToString(data), nil
}

func copyMapData(source string, destination string) error {
    fileFilter := []string{"last_map", "ChargerPos.data", "StartPos.data"}

    os.MkdirAll(destination, os.ModePerm)

    for _, file := range fileFilter {
        srcFile, err := os.Open(source + file)
        if err != nil {
            return err
        }
        defer srcFile.Close()

        os.Remove(destination + file)
        destFile, err := os.Create(destination + file)
        if err != nil {
            return err
        }
        defer destFile.Close()

        if _, err = io.Copy(destFile, srcFile); err != nil {
            return err
        }

        if err = destFile.Sync(); err != nil {
            return err
        }
    }

    return nil
}

func goTo(x int, y int) error {
    var source = fullMapPath
    var destination = rockroboBasePath

    if err := copyMapData(source, destination); err != nil {
        return err
    }

    cmd := exec.Command("service", "rrwatchdoge", "reload")
    if err := cmd.Run(); err != nil {
        return err
    }

    token, err := getMiioToken()
    if err != nil {
        return err
    }
    
    vacuum, err := miio.NewVacuum("127.0.0.1", token)
    if err != nil {
        return err
    }

    vacuum.UpdateStatus()

    fmt.Println("Okay did it!")

    // TODO: go to

    return nil
}

var cleanMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    token, err := getMiioToken()
    if err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }

    fmt.Println(token)
    
    vacuum, err := miio.NewVacuum("127.0.0.1", token)
    if err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }

    command := string(message.Payload())
    if command == "start" {
        vacuum.StartCleaning()
    } else if command == "pause" {
        vacuum.PauseCleaning()
    } else {
        vacuum.StopCleaningAndDock()
    }

    fmt.Println("Okay did it!")

    client.Publish(message.Topic() + "/status", 0, false, "Success")
}

var initMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    var source = rockroboBasePath
    var destination = roomControllerBasePath + string(message.Payload()) + "/"

    if err := copyMapData(source, destination); err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
    } else {
        client.Publish(message.Topic() + "/status", 0, false, "Success")
    }
}

var goToMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    var coordinates Coordinates

    if err := json.Unmarshal(message.Payload(), &coordinates); err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }

    if err := goTo(coordinates.X, coordinates.Y); err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }
    
    client.Publish(message.Topic() + "/status", 0, false, "Success")
}

var sshPubKeyMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    os.Remove(sshPrivateKeyPath)
    os.Remove(sshPublicKeyPath)
    cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", sshPrivateKeyPath, "-C", "vacuum_1", "-q", "-N", "")
    if err := cmd.Run(); err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }

    data, err := ioutil.ReadFile(sshPublicKeyPath)
    if err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }

    client.Publish(message.Topic() + "/status", 0, false, data)
}

var sshTunnelMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    var remoteHost RemoteHost

    if err := json.Unmarshal(message.Payload(), &remoteHost); err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
        return
    }

    if remoteHost.FetchKey {
        file, err := os.OpenFile(sshKnownHostsPath, os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
            client.Publish(message.Topic() + "/status", 0, false, err.Error())
            return
        }
        defer file.Close()

        cmd := exec.Command("ssh-keyscan", "-H", "-p", remoteHost.Port, remoteHost.Address)
        cmd.Stdout = file
        if err := cmd.Run(); err != nil {
            client.Publish(message.Topic() + "/status", 0, false, err.Error())
            return
        }
    }

    cmd := exec.Command("ssh", "-f", "-N", "-T", "-R52222:localhost:22", mqttUsername + "@" + remoteHost.Address, "-p", remoteHost.Port)
    if err := cmd.Run(); err != nil {
        client.Publish(message.Topic() + "/status", 0, false, err.Error())
    }

    client.Publish(message.Topic() + "/status", 0, false, "Success")
}

func main() {
    signalChannel := make(chan os.Signal, 1)
    signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

    opts := mqtt.NewClientOptions().AddBroker(mqttServer)
    opts.SetClientID(mqttClientId)
    opts.SetUsername(mqttUsername).SetPassword(mqttPassword)

    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    if token := client.Subscribe("devices/vacuum/1/clean", 0, cleanMsgRcvd); token.Wait() && token.Error() != nil {
        fmt.Println(token.Error())
    }

    if token := client.Subscribe("devices/vacuum/1/init", 0, initMsgRcvd); token.Wait() && token.Error() != nil {
        fmt.Println(token.Error())
    }

    if token := client.Subscribe("devices/vacuum/1/goto", 0, goToMsgRcvd); token.Wait() && token.Error() != nil {
        fmt.Println(token.Error())
    }

    if token := client.Subscribe("devices/vacuum/1/ssh/pubkey", 0, sshPubKeyMsgRcvd); token.Wait() && token.Error() != nil {
        fmt.Println(token.Error())
    }

    if token := client.Subscribe("devices/vacuum/1/ssh/tunnel", 0, sshTunnelMsgRcvd); token.Wait() && token.Error() != nil {
        fmt.Println(token.Error())
    }

    <- signalChannel
}
