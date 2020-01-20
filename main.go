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
    "time"

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

var subscriptions = map[string]MqttMsgHandler{
    "devices/vacuum/1/save_map": saveMapMsgRcvd,
    "devices/vacuum/1/clean": cleanMsgRcvd,
    "devices/vacuum/1/goto_target": gotoTargetMsgRcvd,
    "devices/vacuum/1/clean_room": cleanRoomMsgRcvd,

    "devices/vacuum/1/ssh/pubkey": sshPubKeyMsgRcvd,
    "devices/vacuum/1/ssh/tunnel": sshTunnelMsgRcvd,
}

type MqttMsgHandler func(client mqtt.Client, message mqtt.Message) (*string, error)

type Coordinates []int

type Room struct {
    Zones       RoomZones   `json:"zones"`
    IdlePoint   Coordinates `json:"idle_point"`
}

type RoomZones [][]int

type StatusRespone struct {
    Error   *string     `json:"error"`
    Data    interface{} `json:"data"`
}

type RemoteHost struct {
    Address     string
    Port        string
    FetchKey    bool
}

var Vacuum *miio.Vacuum

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

func restoreFullMap() error {
    var source = fullMapPath
    var destination = rockroboBasePath

    if err := copyMapData(source, destination); err != nil {
        return err
    }

    cmd := exec.Command("service", "rrwatchdoge", "reload")
    if err := cmd.Run(); err != nil {
        return err
    }

    time.Sleep(30 * time.Second)

    return nil
}

func gotoTarget(x int, y int) error {
    if err := restoreFullMap(); err != nil {
        return err
    }

    Vacuum.GotoTarget(x, y)

    fmt.Println("Going to the target point.")

    return nil
}

func cleanRoom(zones RoomZones, idlePoint Coordinates) error {
    if err := restoreFullMap(); err != nil {
        return err
    }

    Vacuum.ZonedClean(zones)

    fmt.Println("Starting zoned clean.")

    return nil
}

var saveMapMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    var source = rockroboBasePath
    var destination = roomControllerBasePath + string(message.Payload()) + "/"

    if err := copyMapData(source, destination); err != nil {
        return nil, err
    }

    return nil, nil
}

var cleanMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    command := string(message.Payload())
    if command == "start" {
        Vacuum.StartCleaning()
    } else if command == "pause" {
        Vacuum.PauseCleaning()
    } else {
        Vacuum.StopCleaningAndDock()
    }

    return nil, nil
}

var gotoTargetMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    var coordinates Coordinates

    if err := json.Unmarshal(message.Payload(), &coordinates); err != nil {
        return nil, err
    }

    if err := gotoTarget(coordinates[0], coordinates[1]); err != nil {
        return nil, err
    }

    return nil, nil
}

var cleanRoomMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    var room Room

    if err := json.Unmarshal(message.Payload(), &room); err != nil {
        return nil, err
    }

    if err := cleanRoom(room.Zones, room.IdlePoint); err != nil {
        return nil, err
    }
    
    return nil, nil
}

var sshPubKeyMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    os.Remove(sshPrivateKeyPath)
    os.Remove(sshPublicKeyPath)
    cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", sshPrivateKeyPath, "-C", "vacuum_1", "-q", "-N", "")
    if err := cmd.Run(); err != nil {
        return nil, err
    }

    data, err := ioutil.ReadFile(sshPublicKeyPath)
    if err != nil {
        return nil, err
    }

    str_data := string(data)

    return &str_data, nil
}

var sshTunnelMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    var remoteHost RemoteHost

    if err := json.Unmarshal(message.Payload(), &remoteHost); err != nil {
        return nil, err
    }

    if remoteHost.FetchKey {
        file, err := os.OpenFile(sshKnownHostsPath, os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
            return nil, err
        }
        defer file.Close()

        cmd := exec.Command("ssh-keyscan", "-H", "-p", remoteHost.Port, remoteHost.Address)
        cmd.Stdout = file
        if err := cmd.Run(); err != nil {
            return nil, err
        }
    }

    cmd := exec.Command("ssh", "-f", "-N", "-T", "-R52222:localhost:22", mqttUsername + "@" + remoteHost.Address, "-p", remoteHost.Port)
    if err := cmd.Run(); err != nil {
        return nil, err
    }

    return nil, nil
}

var mqttMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    var str_error *string

    fmt.Println("MQTT message received!")

    data, err := subscriptions[message.Topic()](client, message)
    if err != nil {
        tmp := err.Error(); str_error = &tmp
    }

    statusResponse := StatusRespone{
        Error: str_error,
        Data: data,
    }

    jdata, err := json.Marshal(statusResponse)
    if err != nil {
        client.Publish(message.Topic() + "/status", 0, false, `{"error":"` + err.Error() + `","data":null}`)
        return
    }

    client.Publish(message.Topic() + "/status", 0, false, string(jdata))
}

var pingMsgRcvd = func(client mqtt.Client, message mqtt.Message) {
    fmt.Println("PING message received!")

    token := client.Publish(message.Topic() + "/status", 0, false, "PONG")
    if token.Wait() && token.Error() != nil {
        panic(token.Error())
    }
}

func onConnected(client mqtt.Client) {
    if token := client.Subscribe("devices/vacuum/1/ping", 0, pingMsgRcvd); token.Wait() && token.Error() != nil {
        fmt.Println(token.Error())
    }

    for topic := range subscriptions {
        if token := client.Subscribe(topic, 0, mqttMsgRcvd); token.Wait() && token.Error() != nil {
            fmt.Println(token.Error())
        }
    }
}

func main() {
    signalChannel := make(chan os.Signal, 1)
    signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

    opts := mqtt.NewClientOptions()
    opts.SetAutoReconnect(true)
    opts.SetCleanSession(true)
    opts.SetConnectTimeout(0)
    opts.SetMaxReconnectInterval(5 * time.Second)
    opts.SetOnConnectHandler(onConnected)

    opts.AddBroker(mqttServer)
    opts.SetClientID(mqttClientId)
    opts.SetUsername(mqttUsername)
    opts.SetPassword(mqttPassword)

    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    token, err := getMiioToken()
    if err != nil {
        fmt.Println("Error: " + err.Error())
        return
    }

    Vacuum, err = miio.NewVacuum("127.0.0.1", token)
    if err != nil {
        fmt.Println("Error: " + err.Error())
        return
    }

    <- signalChannel

    fmt.Println("Goodbye!")
}
