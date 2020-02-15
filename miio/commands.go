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

// Gateway device definition.
type deviceDTO struct {
    Sid   string `json:"sid,omitempty"`
    Model string `json:"model,omitempty"`
    Data  string `json:"data,omitempty"`
    Token string `json:"token,omitempty"`
}

// Gateway command.
type command struct {
    deviceDTO
    Cmd string `json:"cmd"`
}

// Independent device command.
type deviceCommand struct {
    ID     int64         `json:"id"`
    Method string        `json:"method"`
    Params []interface{} `json:"params,omitempty"`
}

// Base response from the device.
type devResponse struct {
    ID int64 `json:"id"`
}
