//go:build linux || darwin
// +build linux darwin

package ipc

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// Server create a unix socket and start listening connections - for unix and linux
func (sc *Server) startListen() error {
	base := sc.socketDirectory
	if base == "" {
		base = "/tmp/"
	} else if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	sockPath := base + sc.name + ".sock"

	if err := os.RemoveAll(sockPath); err != nil {
		return err
	}

	var oldUmask int
	if sc.unMask >= 0 {
		oldUmask = syscall.Umask(sc.unMask)
	}

	listen, err := net.Listen("unix", sockPath)

	if sc.unMask >= 0 {
		syscall.Umask(oldUmask)
	}

	if err != nil {
		return err
	}

	sc.listen = listen

	sc.status = Listening

	go sc.acceptLoop()

	return nil

}

// Client connect to the unix socket created by the server -  for unix and linux
func (cc *Client) dial() error {

	base := "/tmp/"
	sock := ".sock"

	startTime := time.Now()

	for {
		if cc.timeout != 0 {
			if time.Now().Sub(startTime).Seconds() > cc.timeout {
				cc.status = Closed
				return errors.New("Timed out trying to connect")
			}
		}

		conn, err := net.Dial("unix", base+cc.Name+sock)
		if err != nil {

			if strings.Contains(err.Error(), "connect: no such file or directory") == true {

			} else if strings.Contains(err.Error(), "connect: Connection refused") == true {

			} else {
				cc.recieved <- &Message{err: err, MsgType: -2}
			}

		} else {

			cc.conn = conn

			err = cc.handshake()
			if err != nil {
				return err
			}

			return nil
		}

		time.Sleep(cc.retryTimer * time.Second)

	}

}
