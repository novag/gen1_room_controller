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
    "net"
)

// Base connection.
type connection struct {
    conn *net.UDPConn

    closeRead  chan bool
    closeWrite chan bool

    inMessages  chan []byte
    outMessages chan []byte

    DeviceMessages chan []byte
}

// Creates a new connection.
func newConnection(ip string, port int) (*connection, error) {
    addr := &net.UDPAddr{
        IP:   net.ParseIP(ip),
        Port: port,
    }

    con, err := net.DialUDP("udp4", nil, addr)
    if err != nil {
        return nil, err
    }

    c := &connection{
        conn:           con,
        closeWrite:     make(chan bool),
        closeRead:      make(chan bool),
        inMessages:     make(chan []byte, 100),
        outMessages:    make(chan []byte, 100),
        DeviceMessages: make(chan []byte, 100),
    }

    c.start()
    return c, nil
}

// Close closes the connection.
func (c *connection) Close() {
    if nil != c.conn {
        c.conn.Close()
    }

    c.closeRead <- true
    c.closeWrite <- true

    close(c.inMessages)
    close(c.outMessages)
    close(c.closeRead)
    close(c.closeWrite)
    close(c.DeviceMessages)
}

// Send sends a new message.
func (c *connection) Send(cmd *command) error {
    out, err := json.Marshal(cmd)
    if err != nil {
        return err
    }
    c.outMessages <- out
    return nil
}

// Starts the listeners.
func (c *connection) start() {
    go c.in()
    go c.out()
}

// Processes incoming messages.
func (c *connection) in() {
    buf := make([]byte, 2048)
    for {
        select {
        case <-c.closeRead:
            return
        default:
            size, _, err := c.conn.ReadFromUDP(buf)
            if err != nil {
                fmt.Printf("Error: Error reading from UDP: %s\n", err.Error())
                continue
            }

            //fmt.Printf("Received msg: size=%d\n", size)

            if size > 0 {
                //fmt.Printf("Received device message: %s\n", string(buf[0:size]))
                msg := make([]byte, size)
                copy(msg, buf[0:size])
                c.DeviceMessages <- msg
            }
        }
    }
}

// Processes outgoing messages.
func (c *connection) out() {
    for {
        select {
        case <-c.closeWrite:
            return
        case msg, ok := <-c.outMessages:
            if !ok {
                fmt.Println("Error: not ok, returning.")
                return
            }

            //fmt.Printf("Sending msg %s\n", string(msg))
            _, err := c.conn.Write(msg)
            if err != nil {
                fmt.Printf("Error: Error writing to UDP: %s\n", err.Error())
            }
        }
    }
}
