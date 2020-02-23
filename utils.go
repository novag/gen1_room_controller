package main

import (
    "crypto/md5"
    "encoding/hex"
    "io"
    "io/ioutil"
    "math/rand"
    "net"
    "os"
    "strconv"
    "strings"
)

func randInt(low int, high int) int {
    return low + rand.Intn(high - low)
}

func GetIdentifier() (string, error) {
    iface, err := net.InterfaceByName("wlan0")
    if err != nil {
        return "iderr", err
    }

    return strings.ReplaceAll(iface.HardwareAddr.String(), ":", ""), nil
}

func GetClientId(prefix string) string {
    var clientId strings.Builder

    if prefix != "" {
        clientId.WriteString(prefix)
        clientId.WriteString("_")
    }

    identifier, err := GetIdentifier()
    clientId.WriteString(identifier)
    if err != nil {
        clientId.WriteString("_")
        clientId.WriteString(strconv.Itoa(randInt(10000000, 99999999)))
    }

    return clientId.String()
}

func GetMiioToken() (string, error) {
    data, err := ioutil.ReadFile(miioTokenPath)
    if err != nil {
        return "", err
    }

    data = data[:16]

    return hex.EncodeToString(data), nil
}

func FileChecksum(filepath string) (string, error) {
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
