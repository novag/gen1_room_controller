/*
 * Copyright (c) 2018 Vlad Korniev - Original work
 * Copyright (c) 2020 Hendrik Hagendorn - Modified work
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
package miio

import (
    "encoding/json"
    "fmt"
    "time"
)

// Commands
const (
    cmdGetStatus    = "get_status"
    cmdStart        = "app_start"
    cmdGotoTarget   = "app_goto_target"
    cmdZonedClean   = "app_zoned_clean"
    cmdStop         = "app_stop"
    cmdPause        = "app_pause"
    cmdDock         = "app_charge"
    cmdFindMe       = "find_me"
    cmdFanPower     = "set_custom_mode"
    cmdChangeVolume = "change_sound_volume"
)

const (
    // Number of command retries.
    vacRetries = 3
)

// VacError defines possible vacuum error.
type VacError int

const (
    // VacErrorNo describes no errors.
    VacErrorNo VacError = iota
    // VacErrorCharge describes error with charger.
    VacErrorCharge
    // VacErrorFull describes full dust container.
    VacErrorFull
    // VacErrorUnknown describes unknown error
    VacErrorUnknown
)

// VacState defines possible vacuum state.
type VacState int

const (
    // VacStateUnknown describes unknown state.
    VacStateUnknown VacState = iota
    // VacStateInitiating indicates that vacuum is in initializing mode.
    VacStateInitiating
    // VacStateSleeping indicates that vacuum is in a sleep mode.
    VacStateSleeping
    // VacStateIdle indicates that vacuum is idle.
    VacStateIdle
    // VacStateRemoteControl indicates that vacuum is remotely controlled.
    VacStateRemoteControl
    // VacStateCleaning indicates that vacuums is cleaning.
    VacStateCleaning
    // VacStateReturning indicates that vacuum is returning to the dock.
    VacStateReturning
    // VacStateManualMode
    VacStateManualMode
    // VacStateCharging indicates that vacuum is charging.
    VacStateCharging
    // VacStateChargingError indicates that vacuum has charging issues.
    VacStateChargingError
    // VacStatePaused indicates that cleaning is paused.
    VacStatePaused
    // VacStateSpot indicates that vacuum is cleaning a spot.
    VacStateSpot
    // VacStateInError indicates that vacuum is in error state.
    VacStateInError
    //VacStateShuttingDown indicates that vacuum is shutting down.
    VacStateShuttingDown
    // VacStateUpdating indicates that vacuum is in an update mode.
    VacStateUpdating
    // VacStateDocking indicates that vacuum is in a process of docking.
    VacStateDocking
    // VacStateGoTo indicates that vacuum is going to a target point.
    VacStateGoTo
    // VacStateZoneClean indicates that vacuum is cleaning a zone.
    VacStateZoneClean
    // VacStateRoomClean indicates that vacuum is cleaning a room.
    VacStateRoomClean
    // VacStateFullyCharged indicates that vacuum is fully charged.
    VacStateFullyCharged
)

// VacuumState describes a vacuum state.
type VacuumState struct {
    Battery    int
    CleanArea  int
    CleanTime  int
    IsDND      bool
    IsCleaning bool
    FanPower   int
    Error      VacError
    State      VacState
}

// Vacuum state obtained from the device.
type internalState struct {
    Battery    int `json:"battery"`
    CleanArea  int `json:"clean_area"`
    CleanTime  int `json:"clean_time"`
    DNDEnabled int `json:"dnd_enabled"`
    ErrorCode  int `json:"error_code"`
    Cleaning   int `json:"cleaning"`
    FanPower   int `json:"fan_power"`
    MapPresent int `json:"map_present"`
    MsgVer     int `json:"msg_ver"`
    MsgSeq     int `json:"msg_seq"`
    State      int `json:"state"`
}

// Response from the vacuum.
type stateResponse struct {
    Result []*internalState `json:"result"`
}

// DeviceUpdateMessage contains data about an update.
type DeviceUpdateMessage struct {
    ID    string
    State *VacuumState
}

// Vacuum defines a Xiaomi vacuum cleaner.
type Vacuum struct {
    XiaomiDevice
    State *VacuumState

    UpdateChan chan *DeviceUpdateMessage
}

// NewVacuum creates a new vacuum.
func NewVacuum(deviceIP, token string) (*Vacuum, error) {
    v := &Vacuum{
        State: &VacuumState{},
        XiaomiDevice: XiaomiDevice{
            rawState: make(map[string]interface{}),
        },
    }

    err := v.start(deviceIP, token, defaultPort)
    if err != nil {
        return nil, err
    }

    go v.processUpdates()
    v.UpdateChan = make(chan *DeviceUpdateMessage, 100)
    return v, nil
}

// Stop stops the device.
func (v *Vacuum) Stop() {
    v.stop()
    close(v.UpdateChan)
}

// GetUpdateMessage returns an update message.
func (v *Vacuum) GetUpdateMessage() *DeviceUpdateMessage {
    return &DeviceUpdateMessage{
        ID:    v.deviceID,
        State: v.State,
    }
}

// UpdateState performs a state update.
func (v *Vacuum) UpdateState() {
    v.Lock()
    defer v.Unlock()

    b, ok := v.rawState[cmdGetStatus]
    if !ok {
        return
    }

    r := &stateResponse{}
    err := json.Unmarshal(b.([]byte), r)
    if err != nil {
        fmt.Printf("Error: Failed to un-marshal vacuum response: %s\n", err.Error())
        return
    }

    if 0 == len(r.Result) {
        return
    }

    v.State.Battery = r.Result[0].Battery
    v.State.CleanArea = r.Result[0].CleanArea
    v.State.CleanTime = r.Result[0].CleanTime
    v.State.IsDND = r.Result[0].DNDEnabled != 0
    v.State.IsCleaning = r.Result[0].Cleaning != 0
    v.State.FanPower = r.Result[0].FanPower

    switch r.Result[0].ErrorCode {
    case 0:
        v.State.Error = VacErrorNo
    case 9:
        v.State.Error = VacErrorCharge
    case 100:
        v.State.Error = VacErrorFull
    default:
        v.State.Error = VacErrorUnknown
    }

    switch r.Result[0].State {
    case 1:
        v.State.State = VacStateInitiating
    case 2:
        v.State.State = VacStateSleeping
    case 3:
        v.State.State = VacStateIdle
    case 4:
        v.State.State = VacStateRemoteControl
    case 5:
        v.State.State = VacStateCleaning
    case 6:
        v.State.State = VacStateReturning
    case 7:
        v.State.State = VacStateManualMode
    case 8:
        v.State.State = VacStateCharging
    case 9:
        v.State.State = VacStateChargingError
        v.State.Error = VacErrorCharge
    case 10:
        v.State.State = VacStatePaused
    case 11:
        v.State.State = VacStateSpot
    case 12:
        v.State.State = VacStateInError
    case 13:
        v.State.State = VacStateShuttingDown
    case 14:
        v.State.State = VacStateUpdating
    case 15:
        v.State.State = VacStateDocking
    case 16:
        v.State.State = VacStateGoTo
    case 17:
        v.State.State = VacStateZoneClean
    case 18:
        v.State.State = VacStateRoomClean
    case 100:
        v.State.State = VacStateFullyCharged
    default:
        v.State.State = VacStateUnknown
    }

    select {
        case v.UpdateChan <- v.GetUpdateMessage():
        default:
    }
}

// UpdateStatus requests for a state update.
func (v *Vacuum) UpdateStatus() bool {
    return v.sendCommand(cmdGetStatus, nil, true, vacRetries)
}

// StartCleaning starts the cleaning cycle.
func (v *Vacuum) StartCleaning() bool {
    if !v.sendCommand(cmdStart, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// GotoTarget goes to the given target coordinates.
func (v *Vacuum) GotoTarget(x int, y int) bool {
    if !v.sendCommand(cmdGotoTarget, []interface{}{x, y}, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// ZonedClean cleans the given zones n times.
func (v *Vacuum) ZonedClean(zones [][]int) bool {
    _zones := make([]interface{}, len(zones))
    for index, zone := range zones {
        _zones[index] = zone
    }

    if !v.sendCommand(cmdZonedClean, _zones, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// PauseCleaning pauses the cleaning cycle.
func (v *Vacuum) PauseCleaning() bool {
    if !v.sendCommand(cmdPause, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// StopCleaning stops the cleaning cycle.
func (v *Vacuum) StopCleaning() bool {
    if !v.sendCommand(cmdStop, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// StopCleaningAndDock stops the cleaning cycle and returns to dock.
func (v *Vacuum) StopCleaningAndDock() bool {
    if !v.sendCommand(cmdStop, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    if !v.sendCommand(cmdDock, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// Dock returns to dock.
func (v *Vacuum) Dock() bool {
    if !v.sendCommand(cmdDock, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// FindMe sends the find me command.
func (v *Vacuum) FindMe() bool {
    if !v.sendCommand(cmdFindMe, nil, false, vacRetries) {
        return false
    }

    time.Sleep(1 * time.Second)
    return v.UpdateStatus()
}

// SetFanSpeed sets fan speed
func (v *Vacuum) SetFanPower(val uint8) bool {
    if val > 100 {
        val = 100
    }
    if !v.sendCommand(cmdFanPower, []interface{}{val}, false, vacRetries) {
        return false
    }

    return v.UpdateStatus()
}

// SetVolume sets the sound volume
func (v *Vacuum) SetVolume(val uint8) bool {
    if val > 100 {
        val = 100
    }
    if !v.sendCommand(cmdChangeVolume, []interface{}{val}, false, vacRetries) {
        return false
    }

    return v.UpdateStatus()
}

// Processes internal updates.
// We care only about state update messages.
func (v *Vacuum) processUpdates() {
    for msg := range v.messages {
        m := msg.(string)
        switch m {
        case cmdGetStatus:
            v.UpdateState()
        }
    }
}
