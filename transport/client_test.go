/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package getty

import (
	"bytes"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/stretchr/testify/assert"
)

type PackageHandler struct{}

func (h *PackageHandler) Read(ss Session, data []byte) (interface{}, int, error) {
	return nil, 0, nil
}

func (h *PackageHandler) Write(ss Session, pkg interface{}) ([]byte, error) {
	return nil, nil
}

type MessageHandler struct {
	lock  sync.Mutex
	array []Session
}

func newMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

func (h *MessageHandler) SessionNumber() int {
	h.lock.Lock()
	connNum := len(h.array)
	h.lock.Unlock()

	return connNum
}

func (h *MessageHandler) OnOpen(session Session) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.array = append(h.array, session)

	return nil
}
func (h *MessageHandler) OnError(session Session, err error)         {}
func (h *MessageHandler) OnClose(session Session)                    {}
func (h *MessageHandler) OnMessage(session Session, pkg interface{}) {}
func (h *MessageHandler) OnCron(session Session)                     {}

type Package struct{}

func (p Package) String() string {
	return ""
}
func (p Package) Marshal() (*bytes.Buffer, error)           { return nil, nil }
func (p *Package) Unmarshal(buf *bytes.Buffer) (int, error) { return 0, nil }

func newSessionCallback(session Session, handler *MessageHandler) error {
	var pkgHandler PackageHandler
	session.SetName("hello-client-session")
	session.SetMaxMsgLen(128 * 1024) // max message package length 128k
	session.SetPkgHandler(&pkgHandler)
	session.SetEventListener(handler)
	session.SetReadTimeout(3e9)
	session.SetWriteTimeout(3e9)
	session.SetCronPeriod((int)(30e9 / 1e6))
	session.SetWaitTime(3e9)

	return nil
}

func TestTCPClient(t *testing.T) {
	listenLocalServer := func() (net.Listener, error) {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}

		go http.Serve(listener, nil)
		return listener, nil
	}

	listener, err := listenLocalServer()
	assert.Nil(t, err)
	assert.NotNil(t, listener)

	addr := listener.Addr().(*net.TCPAddr)
	t.Logf("server addr: %v", addr)
	clt := NewTCPClient(
		WithServerAddress(addr.String()),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	// assert.Equal(t, clt.endPointType, TCP_CLIENT)

	var msgHandler MessageHandler
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	ss := msgHandler.array[0]
	ss.setSession(ss)
	_, err = ss.send([]byte("hello"))
	assert.Nil(t, err)
	active := ss.GetActive()
	assert.NotNil(t, active)
	ss.SetCompressType(CompressNone)
	conn := ss.(*session).Connection.(*gettyTCPConn)
	assert.True(t, conn.compress == CompressNone)
	beforeWriteBytes := conn.writeBytes
	beforeWritePkgNum := conn.writePkgNum
	l, err := conn.send([]byte("hello"))
	assert.Nil(t, err)
	assert.True(t, l == 5)
	beforeWritePkgNum.Add(1)
	beforeWriteBytes.Add(5)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	l, err = ss.WriteBytes([]byte("hello"))
	assert.Nil(t, err)
	assert.True(t, l == 5)
	beforeWriteBytes.Add(5)
	beforeWritePkgNum.Add(1)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	var pkgs [][]byte
	pkgs = append(pkgs, []byte("hello"), []byte("hello"))
	l, err = conn.send(pkgs)
	assert.Nil(t, err)
	assert.True(t, l == 10)
	beforeWritePkgNum.Add(2)
	beforeWriteBytes.Add(10)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	ss.SetCompressType(CompressSnappy)
	l, err = ss.WriteBytesArray(pkgs...)
	assert.Nil(t, err)
	assert.True(t, l == 10)
	beforeWritePkgNum.Add(2)
	beforeWriteBytes.Add(10)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	assert.True(t, conn.compress == CompressSnappy)

	batchSize := 128 * 1023
	source := make([]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		source[i] = 't'
	}
	l, err = ss.WriteBytes(source)
	assert.Nil(t, err)
	assert.True(t, l == batchSize)
	beforeWriteBytes.Add(uint32(batchSize))
	beforeWritePkgNum.Add(uint32(batchSize/16/1024) + 1)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)

	batchSize = 32 * 1024
	source = make([]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		source[i] = 't'
	}
	l, err = ss.WriteBytes(source)
	assert.Nil(t, err)
	assert.True(t, l == batchSize)
	beforeWriteBytes.Add(uint32(batchSize))
	beforeWritePkgNum.Add(2)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	assert.Equal(t, time.Duration(3000000000), ss.readTimeout())
	clt.Close()
	assert.True(t, clt.IsClosed())
}

func TestUDPClient(t *testing.T) {
	var (
		err      error
		conn     *net.UDPConn
		sendLen  int
		totalLen int
	)
	func() {
		ip := net.ParseIP("127.0.0.1")
		srcAddr := &net.UDPAddr{IP: ip, Port: 0}
		conn, err = net.ListenUDP("udp", srcAddr)
		assert.Nil(t, err)
		assert.NotNil(t, conn)
	}()
	defer conn.Close()

	addr := conn.LocalAddr()
	t.Logf("server addr: %v", addr)
	clt := NewUDPClient(
		WithServerAddress(addr.String()),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	// assert.Equal(t, clt.endPointType, UDP_CLIENT)

	var msgHandler MessageHandler
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	ss := msgHandler.array[0]
	ss.setSession(ss)
	_, err = ss.send([]byte("hello"))
	assert.NotNil(t, err)
	active := ss.GetActive()
	assert.NotNil(t, active)
	totalLen, sendLen, err = ss.WritePkg(nil, 0)
	assert.NotNil(t, err)
	assert.True(t, sendLen == 0)
	assert.True(t, totalLen == 0)
	totalLen, sendLen, err = ss.WritePkg([]byte("hello"), 0)
	assert.NotNil(t, err)
	assert.True(t, sendLen == 0)
	assert.True(t, totalLen == 0)
	l, err := ss.WriteBytes([]byte("hello"))
	assert.Zero(t, l)
	assert.NotNil(t, err)
	l, err = ss.WriteBytesArray([]byte("hello"))
	assert.Zero(t, l)
	assert.NotNil(t, err)
	l, err = ss.WriteBytesArray([]byte("hello"), []byte("world"))
	assert.Zero(t, l)
	assert.NotNil(t, err)
	ss.SetCompressType(CompressNone)
	host, port, _ := net.SplitHostPort(addr.String())
	if len(host) < 8 {
		host = "127.0.0.1"
	}
	remotePort, _ := strconv.Atoi(port)
	serverAddr := net.UDPAddr{IP: net.ParseIP(host), Port: remotePort}
	udpCtx := UDPContext{
		Pkg:      "hello",
		PeerAddr: &serverAddr,
	}
	t.Logf("udp context:%s", udpCtx)
	udpConn := ss.(*session).Connection.(*gettyUDPConn)
	_, err = udpConn.send(udpCtx)
	assert.NotNil(t, err)
	udpCtx.Pkg = []byte("hello")
	beforeWriteBytes := udpConn.writeBytes
	_, err = udpConn.send(udpCtx)
	beforeWriteBytes.Add(5)
	assert.Equal(t, beforeWriteBytes, udpConn.writeBytes)
	assert.Nil(t, err)

	beforeWritePkgNum := udpConn.writePkgNum
	totalLen, sendLen, err = ss.WritePkg(udpCtx, 0)
	beforeWritePkgNum.Add(1)
	assert.Equal(t, beforeWritePkgNum, udpConn.writePkgNum)
	assert.Nil(t, err)
	assert.True(t, sendLen == 0)
	assert.True(t, totalLen == 0)

	clt.Close()
	assert.True(t, clt.IsClosed())
	msgHandler.array[0].Reset()
	assert.Nil(t, msgHandler.array[0].Conn())
	// ss.WritePkg([]byte("hello"), 0)
}

func TestNewWSClient(t *testing.T) {
	var (
		server           Server
		serverMsgHandler MessageHandler
	)
	addr := "127.0.0.1:65000"
	path := "/hello"
	func() {
		server = NewWSServer(
			WithLocalAddress(addr),
			WithWebsocketServerPath(path),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		go server.RunEventLoop(newServerSession)
	}()
	time.Sleep(1e9)

	client := NewWSClient(
		WithServerAddress("ws://"+addr+path),
		WithConnectionNumber(1),
	)

	var msgHandler MessageHandler
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	client.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	ss := msgHandler.array[0]
	ss.SetCompressType(CompressNone)
	conn := ss.(*session).Connection.(*gettyWSConn)
	assert.True(t, conn.compress == CompressNone)
	err := conn.handlePing("hello")
	assert.Nil(t, err)
	l, err := conn.send("hello")
	assert.NotNil(t, err)
	assert.True(t, l == 0)
	ss.setSession(ss)
	_, err = ss.send([]byte("hello"))
	assert.Nil(t, err)
	active := ss.GetActive()
	assert.NotNil(t, active)
	beforeWriteBytes := conn.writeBytes
	_, err = conn.send([]byte("hello"))
	assert.Nil(t, err)
	beforeWriteBytes.Add(5)
	assert.Equal(t, beforeWriteBytes, conn.writeBytes)
	beforeWritePkgNum := conn.writePkgNum
	l, err = ss.WriteBytes([]byte("hello"))
	assert.Nil(t, err)
	assert.True(t, l == 5)
	beforeWritePkgNum.Add(1)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	l, err = ss.WriteBytesArray([]byte("hello"), []byte("hello"))
	assert.Nil(t, err)
	assert.True(t, l == 10)
	beforeWritePkgNum.Add(2)
	assert.Equal(t, beforeWritePkgNum, conn.writePkgNum)
	err = conn.writePing()
	assert.Nil(t, err)

	done := make(chan int)
	conn.conn.SetCloseHandler(func(code int, text string) error {
		defer func() {
			done <- code
			close(done)
		}()
		message := websocket.FormatCloseMessage(code, "")
		conn.conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(1e9))
		return nil
	})
	serverSession := serverMsgHandler.array[0]
	serverSession.Close()
	select {
	case code := <-done:
		// refer to websocket.isValidReceivedCloseCode
		assert.True(t, (code >= 1000 && code <= 1003) || (code >= 1007 && code <= 1013) || (code >= 3000 && code <= 4999))
	case <-time.After(5e9):
		assert.True(t, false)
	}
	assert.True(t, serverSession.IsClosed())

	ss.SetReader(nil)
	assert.Nil(t, ss.(*session).reader)
	ss.SetWriter(nil)
	assert.Nil(t, ss.(*session).writer)
	assert.Nil(t, ss.(*session).GetAttribute("hello"))

	client.Close()
	assert.True(t, client.IsClosed())
	server.Close()
	assert.True(t, server.IsClosed())
}

var (
	WssServerCRT = []byte(`-----BEGIN CERTIFICATE-----
MIICHjCCAYegAwIBAgIQKpKqamBqmZ0hfp8sYb4uNDANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCB
iQKBgQC5Nxsk6WjeaYazRYiGxHZ5G3FXSlSjV7lZeebItdEPzO8kVPIGCSTy/M5X
Nnpp3uVDFXQub0/O5t9Y6wcuqpUGMOV+XL7MZqSZlodXm0XhNYzCAjZ+URNjTHGP
NXIqdDEG5Ba8SXMOfY6H97+QxugZoAMFZ+N83ggr12IYNO/FbQIDAQABo3MwcTAO
BgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUw
AwEB/zA5BgNVHREEMjAwgglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABhxAA
AAAAAAAAAAAAAAAAAAABMA0GCSqGSIb3DQEBCwUAA4GBAE5dr9q7ORmKZ7yZqeSL
305armc13A7UxffUajeJFujpl2jOqnb5PuKJ7fn5HQKGB0qSq3IHsFua2WONXcTW
Vn4gS0k50IaDpW+yl+ArIo0QwbjPIAcFysX10p9dVO7A1uEpHbRDzefem6r9uVGk
i7dOLEoC8hkfk6nJsNEIEqu6
-----END CERTIFICATE-----`)
	WssServerCRTFile = "/tmp/server.crt"
	WssServerKEY     = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXgIBAAKBgQC5Nxsk6WjeaYazRYiGxHZ5G3FXSlSjV7lZeebItdEPzO8kVPIG
CSTy/M5XNnpp3uVDFXQub0/O5t9Y6wcuqpUGMOV+XL7MZqSZlodXm0XhNYzCAjZ+
URNjTHGPNXIqdDEG5Ba8SXMOfY6H97+QxugZoAMFZ+N83ggr12IYNO/FbQIDAQAB
AoGBAJgvuXQY/fxSxUWkysvBvn9Al17cSrN0r23gBkvBaakMASvfSIbBGMU4COwM
bYV0ivkWNcK539/oQHk1lU85Bv0K9V9wtuFrYW0mN3TU6jnl6eEnzW5oy0Z9TwyY
wuGQOSXGr/aDVu8Wr7eOmSvn6j8rWO2dSMHCllJnSBoqQ1aZAkEA5YQspoMhUaq+
kC53GTgMhotnmK3fWfWKrlLf0spsaNl99W3+plwqxnJbye+5uEutRR1PWSWCCKq5
bN9veOXViwJBAM6WS5aeKO/JX09O0Ang9Y0+atMKO0YjX6fNFE2UJ5Ewzyr4DMZK
TmBpyzm4x/GhV9ukqcDcd3dNlUOtgRqY3+cCQQDCGmssk1+dUpqBE1rT8CvfqYv+
eqWWzerwDNSPz3OppK4630Bqby4Z0GNCP8RAUXgDKIuPqAH11HSm17vNcgqLAkA8
8FCzyUvCD+CxgEoV3+oPFA5m2mnJsr2QvgnzKHTTe1ZhEnKSO3ELN6nfCQbR3AoS
nGwGnAIRiy0wnYmr0tSZAkEAsWFm/D7sTQhX4Qnh15ZDdUn1WSWjBZevUtJnQcpx
TjihZq2sd3uK/XrzG+w7B+cPZlrZtQ94sDSVQwWl/sxB4A==
-----END RSA PRIVATE KEY-----`)
	WssServerKEYFile = "/tmp/server.key"
	WssClientCRT     = []byte(`-----BEGIN CERTIFICATE-----
MIICHjCCAYegAwIBAgIQKpKqamBqmZ0hfp8sYb4uNDANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCB
iQKBgQC5Nxsk6WjeaYazRYiGxHZ5G3FXSlSjV7lZeebItdEPzO8kVPIGCSTy/M5X
Nnpp3uVDFXQub0/O5t9Y6wcuqpUGMOV+XL7MZqSZlodXm0XhNYzCAjZ+URNjTHGP
NXIqdDEG5Ba8SXMOfY6H97+QxugZoAMFZ+N83ggr12IYNO/FbQIDAQABo3MwcTAO
BgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUw
AwEB/zA5BgNVHREEMjAwgglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABhxAA
AAAAAAAAAAAAAAAAAAABMA0GCSqGSIb3DQEBCwUAA4GBAE5dr9q7ORmKZ7yZqeSL
305armc13A7UxffUajeJFujpl2jOqnb5PuKJ7fn5HQKGB0qSq3IHsFua2WONXcTW
Vn4gS0k50IaDpW+yl+ArIo0QwbjPIAcFysX10p9dVO7A1uEpHbRDzefem6r9uVGk
i7dOLEoC8hkfk6nJsNEIEqu6
-----END CERTIFICATE-----`)
	WssClientCRTFile = "/tmp/client.crt"
)

func DownloadFile(filepath string, content []byte) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = out.Write(content)
	return err
}

func TestNewWSSClient(t *testing.T) {
	var (
		err              error
		server           Server
		serverMsgHandler MessageHandler
	)

	os.Remove(WssServerCRTFile)
	err = DownloadFile(WssServerCRTFile, WssServerCRT)
	assert.Nil(t, err)
	defer os.Remove(WssServerCRTFile)

	os.Remove(WssServerKEYFile)
	err = DownloadFile(WssServerKEYFile, WssServerKEY)
	assert.Nil(t, err)
	defer os.Remove(WssServerKEYFile)

	os.Remove(WssClientCRTFile)
	err = DownloadFile(WssClientCRTFile, WssClientCRT)
	assert.Nil(t, err)
	defer os.Remove(WssClientCRTFile)

	addr := "127.0.0.1:63450"
	path := "/hello"
	func() {
		server = NewWSSServer(
			WithLocalAddress(addr),
			WithWebsocketServerPath(path),
			WithWebsocketServerCert(WssServerCRTFile),
			WithWebsocketServerPrivateKey(WssServerKEYFile),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		go server.RunEventLoop(newServerSession)
	}()
	time.Sleep(1e9)

	client := NewWSSClient(
		WithServerAddress("wss://"+addr+path),
		WithConnectionNumber(1),
		WithRootCertificateFile(WssClientCRTFile),
	)

	var msgHandler MessageHandler
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	client.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	client.Close()
	assert.True(t, client.IsClosed())
	assert.False(t, server.IsClosed())
	// time.Sleep(1000e9)
	// server.Close()
	// assert.True(t, server.IsClosed())
}
