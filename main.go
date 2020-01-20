package main

import (
    "crypto/md5"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "io/ioutil"
    "os"
    "os/exec"
    "os/signal"
    "strconv"
    "sync"
    "syscall"
    "time"

    "github.com/novag/gen1_room_controller/miio"
    "github.com/eclipse/paho.mqtt.golang"
)

const (
    miioTokenPath = "/mnt/data/miio/device.token"
    rockroboBasePath = "/mnt/data/rockrobo/"
    roomControllerBasePath = "/mnt/data/room_controller/"
    baseMapPath = roomControllerBasePath + "full/"
    sshPrivateKeyPath = "/root/.ssh/id_ed25519"
    sshPublicKeyPath = "/root/.ssh/id_ed25519.pub"
    sshKnownHostsPath = "/root/.ssh/known_hosts"

    statusUpdateTopic = "devices/vacuum/1/status"
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

var copyMapMutex sync.Mutex
var Vacuum *miio.Vacuum

func getMiioToken() (string, error) {
    data, err := ioutil.ReadFile(miioTokenPath)
    if err != nil {
        return "", err
    }

    data = data[:16]

    return hex.EncodeToString(data), nil
}

func md5sum(filepath string) (string, error) {
    var hash string

    file, err := os.Open(filepath)
    if err != nil {
        return hash, err
    }
    defer file.Close()

    h := md5.New()
    if _, err := io.Copy(h, file); err != nil {
        return hash, err
    }

    hash = hex.EncodeToString(h.Sum(nil))

    return hash, nil
}

func checkDocked() error {
    state := Vacuum.GetUpdateMessage().State.State

    if state == miio.VacStateCharging || state == miio.VacStateFullyCharged {
        return nil
    }

    return errors.New("Vacuum not docked! - State: " + strconv.Itoa(int(state)))
}

func checkAvailable() error {
    state := Vacuum.GetUpdateMessage().State.State

    if state == miio.VacStateCharging || state == miio.VacStateFullyCharged ||
            state == miio.VacStateIdle || state == miio.VacStateSleeping ||
            state == miio.VacStatePaused {
        return nil;
    }

    return errors.New("Vacuum busy! - State: " + strconv.Itoa(int(state)))
}

func copyMapData(source string, destination string) (bool, error) {
    fileFilter := []string{"last_map", "ChargerPos.data", "StartPos.data"}

    // Only allow one call at a time
    copyMapMutex.Lock()
    defer copyMapMutex.Unlock()

    if err := checkDocked(); err != nil {
        return false, err
    }

    for index, file := range fileFilter {
        sourceHash, err := md5sum(source + file)
        if err != nil {
            break
        }

        destinationHash, err := md5sum(destination + file)
        if err != nil {
            break
        }

        if sourceHash != destinationHash {
            break
        }

        if index == len(fileFilter) - 1 {
            return false, nil
        }
    }

    os.MkdirAll(destination, os.ModePerm)

    for _, file := range fileFilter {
        srcFile, err := os.Open(source + file)
        if err != nil {
            return false, err
        }
        defer srcFile.Close()

        os.Remove(destination + file)
        destFile, err := os.Create(destination + file)
        if err != nil {
            return false, err
        }
        defer destFile.Close()

        if _, err = io.Copy(destFile, srcFile); err != nil {
            return false, err
        }

        if err = destFile.Sync(); err != nil {
            return false, err
        }
    }

    return true, nil
}

func restoreBaseMap() error {
    var source = baseMapPath
    var destination = rockroboBasePath

    fmt.Println("Restoring base map!")

    reload, err := copyMapData(source, destination)
    if !reload {
        if err == nil {
            fmt.Println("Map has already been restored!")
        }

        return err
    }

    cmd := exec.Command("service", "rrwatchdoge", "reload")
    if err := cmd.Run(); err != nil {
        return err
    }

    time.Sleep(30 * time.Second)

    fmt.Println("Map restored!")

    return nil
}

func gotoTarget(x int, y int) error {
    Vacuum.GotoTarget(x, y)

    fmt.Println("Going to the target point.")

    return nil
}

func cleanRoom(zones RoomZones, idlePoint Coordinates) error {
    if err := restoreBaseMap(); err != nil {
        return err
    }

    Vacuum.ZonedClean(zones)

    fmt.Println("Starting zoned clean.")

    go func() {
        returnCount := 0
        lastState := miio.VacStateZoneClean

        time.Sleep(30 * time.Second)

        for {
            state := (<-Vacuum.UpdateChan).State.State

            // Done if charging
            if state == miio.VacStateCharging {
                fmt.Println("Charging. All done.")
                return
            }

            if state == lastState {
                continue
            }

            fmt.Printf("Processing state: %d\n", state)

            switch state {
            case miio.VacStateReturning:
                // expect { miio.VacStateIdle }
            case miio.VacStateIdle:
                switch returnCount {
                case 0:
                    // Dock not found
                    time.Sleep(5 * time.Second)

                    gotoTarget(idlePoint[0], idlePoint[1])

                    // expect { miio.VacStateGoTo }
                case 1:
                    // Waiting for docking command

                    // expect { miio.VacStateReturning }
                case 2:
                    // First orientation drive
                    Vacuum.Dock()
                    Vacuum.SetVolume(0)

                    // expect { miio.VacStateReturning }
                case 3:
                    // Second orientation drive
                    Vacuum.Dock()

                    // expect { miio.VacStateReturning }
                case 4:
                    // We should have updated our map, going home now
                    Vacuum.Dock()
                    Vacuum.SetVolume(100)

                    // expect { miio.VacStateReturning }
                case 5:
                    // Let's try one last time
                    Vacuum.Dock()

                    // expect { miio.VacStateReturning }
                default:
                    return
                }

                returnCount++
            case miio.VacStateGoTo:
                // expect { miio.VacStateIdle }
            }

            lastState = state
        }
    }()

    return nil
}

var saveMapMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    if err := checkDocked(); err != nil {
        return nil, err
    }

    var source = rockroboBasePath
    var destination = roomControllerBasePath + string(message.Payload()) + "/"

    if _, err := copyMapData(source, destination); err != nil {
        return nil, err
    }

    return nil, nil
}

var cleanMsgRcvd = func(client mqtt.Client, message mqtt.Message) (*string, error) {
    if err := checkAvailable(); err != nil {
        return nil, err
    }

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

    if err := checkAvailable(); err != nil {
        return nil, err
    }

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

    if err := checkDocked(); err != nil {
        return nil, err
    }

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

func statusUpdateLoop(client mqtt.Client) {
    state := miio.VacStateUnknown

    for {
        Vacuum.UpdateStatus()
        updateMessage := Vacuum.GetUpdateMessage()

        if state != updateMessage.State.State {
            client.Publish(statusUpdateTopic, 0, false, strconv.Itoa(int(state)))

            if state != miio.VacStateCharging && state != miio.VacStateFullyCharged &&
                    updateMessage.State.State == miio.VacStateCharging {
                if err := restoreBaseMap(); err != nil {
                    fmt.Printf("statusUpdateLoop: %s", err.Error())
                }
            }

            state = updateMessage.State.State
            fmt.Printf("New state: %d\n", state)
        }

        time.Sleep(2 * time.Second)
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
    argv := os.Args
    argc := len(argv)

    if argc > 2 {
        fmt.Println("Too many argument.")
        os.Exit(1)
    }

    if argc == 2 {
        switch argv[1] {
        case "setup":
            if err := setup(); err != nil {
                fmt.Println(err.Error())
                os.Exit(1)
            }

            os.Exit(0)
        case "reset":
            if err := reset(); err != nil {
                fmt.Println(err.Error())
                os.Exit(1)
            }

            os.Exit(0)
        default:
            fmt.Println("Unknown argument.")
        }
    }

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

    go statusUpdateLoop(client)

    <- signalChannel

    fmt.Println("Goodbye!")
}
