package ipc

import (
	"bufio"
	"errors"
	"time"
)

// StartServer - starts the ipc server.
//
// ipcName = is the name of the unix socket or named pipe that will be created.
// timeout = number of seconds before the socket/pipe times out waiting for a Connection/re-cconnection - if -1 or 0 it never times out.
//
func StartServer(ipcName string, config *ServerConfig) (*Server, error) {

	err := checkIpcName(ipcName)
	if err != nil {
		return nil, err
	}

	sc := &Server{
		name:     ipcName,
		status:   NotConnected,
		recieved: make(chan *Message),
		unMask:   -1,
	}

	if config == nil {
		sc.timeout = 0
		sc.maxMsgSize = maxMsgSize
		sc.encryption = true

	} else {

		if config.Timeout < 0 {
			sc.timeout = 0
		} else {
			sc.timeout = config.Timeout
		}

		if config.MaxMsgSize < 1024 {
			sc.maxMsgSize = maxMsgSize
		} else {
			sc.maxMsgSize = config.MaxMsgSize
		}

		if config.Encryption == false {
			sc.encryption = false
		} else {
			sc.encryption = true
		}

		if config.UseUnmask {
			sc.unMask = config.Unmask
		}

		sc.securityDescriptor = config.SecurityDescriptor
	}

	go startServer(sc)

	return sc, err
}

func startServer(sc *Server) {
	err := sc.startListen()
	if err != nil {
		sc.recieved <- &Message{err: err, MsgType: -2}
	} else {
		sc.recieved <- &Message{Status: sc.status.String(), MsgType: -1}
	}
}

func (sc *Server) acceptLoop() {
	for {
		conn, err := sc.listen.Accept()
		if err != nil {
			break
		}

		connection := &Connection{
			maxMsgSize: sc.maxMsgSize,
			conn:       conn,
			status:     Connecting,
			toWrite:    make(chan *Message),
		}

		sc.recieved <- &Message{
			MsgType:    -2,
			Connection: connection,
			Status:     connection.status.String(),
		}

		err2 := sc.handshake(connection)
		if err2 != nil {
			sc.recieved <- &Message{err: err2, MsgType: -2}
			conn.Close()
		} else {
			go sc.read(connection)
			go sc.write(connection)

			connection.status = Connected

			sc.recieved <- &Message{
				MsgType:    -2,
				Connection: connection,
				Status:     connection.status.String(),
			}
		}
	}

}

func (sc *Server) read(connection *Connection) {

	bLen := make([]byte, 4)

	for {

		res := sc.readData(connection, bLen)
		if res == false {
			break
		}

		mLen := bytesToInt(bLen)

		msgRecvd := make([]byte, mLen)

		res = sc.readData(connection, msgRecvd)
		if res == false {
			break
		}

		if sc.encryption == true {
			msgFinal, err := decrypt(*connection.enc.cipher, msgRecvd)
			if err != nil {
				sc.recieved <- &Message{Connection: connection, err: err, MsgType: -2}
				continue
			}

			if bytesToInt(msgFinal[:4]) == 0 {
				//  type 0 = control message
			} else {
				sc.recieved <- &Message{Connection: connection, Data: msgFinal[4:], MsgType: bytesToInt(msgFinal[:4])}
			}

		} else {
			if bytesToInt(msgRecvd[:4]) == 0 {
				//  type 0 = control message
			} else {
				sc.recieved <- &Message{Connection: connection, Data: msgRecvd[4:], MsgType: bytesToInt(msgRecvd[:4])}
			}
		}

	}
}

func (sc *Server) readData(connection *Connection, buff []byte) bool {
	_, err := connection.conn.Read(buff)
	if err != nil {

		oldStatus := connection.status
		connection.status = Closed

		sc.recieved <- &Message{Connection: connection, Status: connection.status.String(), MsgType: -1}

		if oldStatus == Closing {
			sc.recieved <- &Message{Connection: connection, err: errors.New("Server has closed the Connection"), MsgType: -2}
			return false
		}

		connection.Close()

		return false
	}

	return true
}

// Read - blocking function that waits until an non multipart message is recieved

func (sc *Server) Read() (*Message, error) {

	m, ok := (<-sc.recieved)
	if ok == false {
		return nil, errors.New("the recieve channel has been closed")
	}

	if m.err != nil {
		close(sc.recieved)
		return nil, m.err
	}

	return m, nil

}

// Write - writes a non multipart message to the ipc Connection.
// msgType - denotes the type of data being sent. 0 is a reserved type for internal messages and errors.
//
func (connection *Connection) Write(msgType int, message []byte) error {

	if msgType == 0 {
		return errors.New("Message type 0 is reserved")
	}

	mlen := len(message)

	if mlen > connection.maxMsgSize {
		return errors.New("Message exceeds maximum message length")
	}

	if connection.status == Connected {

		connection.toWrite <- &Message{MsgType: msgType, Data: message}

	} else {
		return errors.New(connection.status.String())
	}

	return nil

}

func (sc *Server) write(connection *Connection) {

	for {

		m, ok := <-connection.toWrite

		if ok == false {
			break
		}

		toSend := intToBytes(m.MsgType)

		writer := bufio.NewWriter(connection.conn)

		if sc.encryption == true {
			toSend = append(toSend, m.Data...)
			toSendEnc, err := encrypt(*connection.enc.cipher, toSend)
			if err != nil {
				//return err
			}
			toSend = toSendEnc
		} else {

			toSend = append(toSend, m.Data...)

		}

		writer.Write(intToBytes(len(toSend)))
		writer.Write(toSend)

		err := writer.Flush()
		if err != nil {
			//return err
		}

		time.Sleep(2 * time.Millisecond)

	}

}

// getStatus - get the current status of the Connection
func (sc *Server) getStatus() Status {

	return sc.status

}

// StatusCode - returns the current Connection status
func (sc *Server) StatusCode() Status {
	return sc.status
}

// Status - returns the current Connection status as a string
func (sc *Server) Status() string {

	return sc.status.String()

}

// Close - closes the Connection
func (sc *Server) Close() {

	sc.status = Closing
	sc.listen.Close()

}

func (connection *Connection) Close() {
	if connection.status == Closed {
		close(connection.toWrite)
	} else {
		connection.status = Closing
		connection.conn.Close()
	}
}
