package driver

import (
	"fmt"
	"time"

	"github.com/tarm/serial"
)

const sizeChannel = 1

var chanSend chan bool
var serialPort *serial.Port

// TransceiverInit : duoc goi khi khoi tao DS
func TransceiverInit(port string) (err error) {
	chanSend = make(chan bool, sizeChannel)
	// setup uart
	configSerial := &serial.Config{Name: port, Baud: 9600}
	serialPort, err = serial.OpenPort(configSerial)
	if err != nil {
		return err
	}
	fmt.Println("Open Port successful")

	go receiverUartRoutine()
	return nil
}

// TransceiverClose : close serial
func TransceiverClose() {
	serialPort.Close()
}

// SendUartPacket : (timeout - ms, timeUsed-ms) gui du lieu dang ContenRepo toi Uart
func SendUartPacket(content ContentRepo, timeout int64) (timeUsed int64, err error) {
	rawData, lenght, ok := convertStructToTXUartArray(content)
	if !ok || lenght == 0 {
		return timeout, fmt.Errorf("Loi: convert Struct to Uart bytes")
	}

	if chanSend == nil {
		return timeout, fmt.Errorf("Loi: kenh truyen chua duoc khoi tao hoac da bi dong")
	}

	timeOut := time.After(time.Duration(timeout) * time.Millisecond)
	t0 := time.Now()

	select {
	case <-timeOut:
		err = fmt.Errorf("Loi: Timeout")
	case chanSend <- true:
		chanSendErr := make(chan error, 1)
		go sendUart(rawData, lenght, chanSendErr)
		err, ok = <-chanSendErr
	}
	timeUsed = time.Since(t0).Nanoseconds() / 1000 // timeUsed: ms
	return
}

// receiver []byte --> ContentRepo
func receiverUartRoutine() {
	var raw []byte
	var l int16
	for {
		raw, l = receiverUart()
		if l > 0 {
			fmt.Println("rx:" + string(raw))
			go sendRXUartArrayToRepo(raw, l)
		}
	}
}

func sendUart(rawData []byte, lenght int16, chanSendErr chan error) {
	fmt.Println("send raw data:", string(rawData), " - len=", lenght, "\n")
	serialPort.Flush()
	_, err := serialPort.Write(rawData)
	<-chanSend
	chanSendErr <- err
}

func receiverUart() ([]byte, int16) {
	var n int
	var err error
	var raw []byte

	header := make([]byte, 1)
	lenghtBytes := make([]byte, 2)
	var lenghtPayload int16
	cmd := make([]byte, 1)
	crc := make([]byte, 1)
	var checkCRC byte
	data := make([]byte, 1)

	for {
		// receive Header 1 byte:
		for {
			n, err = serialPort.Read(header)
			if err != nil {
				return nil, 0
			}
			if n < 1 {
				continue
			}
			if header[0] == headerConst {
				break
			}
		}

		// receive Lenght 2 byte
		n, err = serialPort.Read(data)
		if err != nil {
			return nil, 0
		}
		if n < 1 {
			continue
		}
		lenghtBytes[0] = data[0]

		n, err = serialPort.Read(data)
		if err != nil {
			return nil, 0
		}
		if n < 1 {
			continue
		}
		lenghtBytes[1] = data[0]

		lenghtPayload = (int16(lenghtBytes[0]) << 8) | int16(lenghtBytes[1])
		if lenghtPayload <= 1 {
			continue
		}

		// receive Cmd
		n, err = serialPort.Read(cmd)
		if err != nil {
			return nil, 0
		}
		if n < 1 {
			continue
		}
		// if !checkVaildCmd(int8(cmd[0])) {
		// 	continue
		// }

		// reset checkCRC
		checkCRC = headerConst + lenghtBytes[0] + lenghtBytes[1] + cmd[0]

		// receive payload
		payloadBytes := make([]byte, lenghtPayload-1)
		for i := range payloadBytes {
			n, err = serialPort.Read(data)
			if err != nil || n < 1 {
				return nil, 0
			}
			payloadBytes[i] = data[0]
		}

		// receive CRC
		n, err = serialPort.Read(crc)
		if err != nil {
			return nil, 0
		}
		if n < 1 {
			continue
		}
		// caculate CRC:
		for _, c := range payloadBytes {
			checkCRC += c
		}
		if checkCRC != crc[0] {
			continue
		}
		raw = make([]byte, 0, lenghtPayload+4)
		raw = append(raw, header[0])
		raw = append(raw, lenghtBytes...)
		raw = append(raw, cmd[0])
		raw = append(raw, payloadBytes...)
		raw = append(raw, crc[0])
		break
	}

	return raw, int16(lenghtPayload + 4)
}
