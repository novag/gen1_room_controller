package main

import (
    "io/ioutil"
    "os/exec"
)

const (
    upstartConfig = `# gen1_room_controller

description "gen1_room_controller service"

start on (started rrwatchdoge and net-device-up IFACE!=lo)
respawn
respawn limit unlimited

script
    sleep 5
    exec /usr/local/bin/gen1_room_controller
end script
`
    upstartConfigPath = "/etc/init/gen1_room_controller.conf"
)

func createUpstartConfig(path string) error {
    return ioutil.WriteFile(path, []byte(upstartConfig), 0644)
}

func setup() error {
    if err := createUpstartConfig(upstartConfigPath); err != nil {
        return err
    }

    cmd := exec.Command("start", "gen1_room_controller")
    if err := cmd.Run(); err != nil {
        return err
    }

    return nil
}