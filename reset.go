package main

import (
    "io"
    "os"
)

const (
    myPath = "/usr/local/bin/gen1_room_controller"
    myUpgradePath = "/tmp/updatetemp/usr/local/bin/gen1_room_controller"
    upstartConfigUpgradePath = "/tmp/updatetemp/etc/init/gen1_room_controller.conf"
)

func reset() error {
    if err := createUpstartConfig(upstartConfigUpgradePath); err != nil {
        return err
    }

    // Copy binary
    srcFile, err := os.Open(myPath)
    if err != nil {
        return err
    }
    defer srcFile.Close()

    destFile, err := os.OpenFile(myUpgradePath, os.O_CREATE|os.O_WRONLY, 0755)
    if err != nil {
        return err
    }
    defer destFile.Close()

    if _, err = io.Copy(destFile, srcFile); err != nil {
        return err
    }

    return nil
}