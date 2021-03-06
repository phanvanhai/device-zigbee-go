package driver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/device-zigbee/driver/packet"
)

const headerConst = 0x55

const (
	//CommandCmdConst :
	CommandCmdConst = iota

	//PushEventCmdConst :
	PushEventCmdConst

	//AddObjectCmdConst :
	AddObjectCmdConst

	//DeleteObjectCmdConst :
	DeleteObjectCmdConst

	//ScanCmdConst :
	ScanCmdConst
)

const (
	CommandIDRead   = 0x01
	CommandIDWrite  = 0x02
	CommandIDDelete = 0x03
)

//----------------------------------------------------------------------------------
//-------------------------------- Packet Struct -----------------------------------
//------------------------------ Common Frame ----------------------------

// UARTFrame :	 TX-RX UART frame
type UARTFrame struct {
	Header  byte
	Lenght  int16 // Lenght = len(Payload) + size(Cmd)
	Cmd     byte
	Payload []byte
	CRC     byte
}

// ContentRepo :	RX_UART EdgeX --> Repo
type ContentRepo struct {
	Cmd     int8
	Content interface{}
}

// ResponseCommonFrame :	Repo --> EdgeX
type ResponseCommonFrame struct {
	ObjectInfo
	StatusResponse uint8  `json:"resp"` // tru Push event se khong co StatusResponse
	NameDevice     string `json:"name,omitempty"`
	Description    string `json:"desc,omitempty"`
	AttributeValue
}

//------------------------- Cmd {command zigbee} -------------------------

// CommandFrame :	EdgeX --> Zigbee
type CommandFrame struct {
	ObjectAddress
	CommandID int8 `json:"cmid"` // Get = 0x01, Set = 0x02, Delete = 0x03
	AttributeInfo
	Value interface{} `json:"val,omitempty"`
}

//-------------------- Cmd {add/delete device/group} ---------------------

// ProvisonFrame :	EdgeX --> Zigbee
type ProvisonFrame struct {
	AddressEUI64
	NameDevice string `json:"name,omitempty"`
}

// DeleteObjectFrame :	EdgeX --> Zigbee
type DeleteObjectFrame struct {
	ObjectAddress
}

//-------------------------- Cmd {scan device} ---------------------------

// ScanDeviceFrame :	EdgeX --> Zigbee
type ScanDeviceFrame struct {
	ScanTime int8 `json:"scan"`
}

//----------------------------------------------------------------------------------

func checkVaildCmd(cmd int8) bool {
	if (cmd == CommandCmdConst) || (cmd == AddObjectCmdConst) || (cmd == PushEventCmdConst) ||
		(cmd == DeleteObjectCmdConst) || (cmd == ScanCmdConst) {
		return true
	}
	return false
}

// ConvertUARTFrameToContentRepo : convert bytes received from UARTFrame to ContentRepo
// su dung truoc khi dua vao Repo
func convertUARTFrameToContentRepo(bFrame []byte, lenFrame int16) (nameRepo string, result ContentRepo, ok bool) {
	var rxFrame UARTFrame

	rxFrame.Header = bFrame[0]
	rxFrame.Lenght = (int16(bFrame[1]) << 8) | int16(bFrame[2])
	rxFrame.Cmd = bFrame[3]
	rxFrame.Payload = make([]byte, 0, lenFrame-5)
	rxFrame.Payload = bFrame[4 : lenFrame-1]
	rxFrame.CRC = bFrame[lenFrame-1]

	result.Cmd = int8(rxFrame.Cmd)

	if checkVaildCmd(result.Cmd) == false {
		// fmt.Println("utils 120")
		return "", ContentRepo{}, false
	}
	var content ResponseCommonFrame
	err := json.Unmarshal(rxFrame.Payload, &content)
	if err != nil {
		// fmt.Println("utils 126")
		return "", ContentRepo{}, false
	}
	result.Content = interface{}(content)

	switch result.Cmd {
	case CommandCmdConst:
		obAddr := content.ObjectAddress
		id, ok := Cache().ConvertAddrToIDObject(obAddr)
		if !ok {
			// fmt.Println("utils 136: %+v", obAddr)
			return "", ContentRepo{}, false
		}
		nameRepo = packet.Repo().GetRepoNameByID(id)

	case PushEventCmdConst:
		go PushEventGoroutine(content)
		return "", result, true

	default:
		nameRepo = packet.Repo().GetRepoNameByCMD(result.Cmd)
	}
	return nameRepo, result, true
}

// SendRXUartArrayToRepo : gui UARTFrame dang []byte da nhan toi Repo phu hop
// su dung boi: RecieveUART co the dung no nhu 1 goroutine de gui du lieu da duoc xu ly toi:
// CommandHandler(), Callback(), Push() goroutine, Discovery() goroutine
func sendRXUartArrayToRepo(bFrameIn []byte, lenght int16) {
	bFrame := make([]byte, 0, lenght)
	bFrame = append(bFrame, bFrameIn...)

	nameRepo, content, ok := convertUARTFrameToContentRepo(bFrame, lenght)
	if !ok {
		// fmt.Println("utils 157")
		return
	}
	if nameRepo == "" {
		// fmt.Println("utils 161")
		return
	}
	packet.Repo().SendToRepo(nameRepo, content)
	// fmt.Printf("utils 163: send to repo:%s-content:%+v", nameRepo, content)
}

func serialJson(content ContentRepo) ([]byte, error) {
	payload, err := json.Marshal(&content.Content)
	if err != nil {
		// fmt.Println("err 167")
		return nil, err
	}
	if content.Cmd != CommandCmdConst {
		// fmt.Println("utils 172")
		return payload, nil
	}

	var commandFrame CommandFrame
	_ = json.Unmarshal(payload, &commandFrame)
	if (commandFrame.AttributeInfo == managerSubcribeAttInfo) || (commandFrame.AttributeInfo == managerSubcribeAttInfo) {
		// fmt.Println("utils 179")
		istart := strings.Index(string(payload), "val\":\"") + 6
		iend := strings.LastIndex(string(payload), "\"")
		subStart := payload[:istart]
		subEnd := payload[iend:]
		subConvert := payload[istart:iend]
		fmt.Println("payload:" + string(payload))
		fmt.Println("subStart:" + string(subStart))
		fmt.Println("subConvert:" + string(subConvert))
		fmt.Println("subEnd:" + string(subEnd))
		b, err := base64.StdEncoding.DecodeString(string(subConvert))
		if err != nil {
			// fmt.Printf("err 187:%v", err)
			return nil, err
		}
		result := make([]byte, 0, len(subStart)+len(b)+len(subEnd))
		result = append(result, subStart...)
		result = append(result, b...)
		result = append(result, subEnd...)
		// fmt.Printf("util 193:%v", result)
		return result, nil
	}
	return payload, nil
}

// ConvertStructToTXUartArray : convert any struct to UARTFrame --> []byte
// su dung de: chuyen content nhan tu channel ContentRepo cua SendUART() thanh []byte de gui UART
// su dung boi: SendUART() goroutine
func convertStructToTXUartArray(content ContentRepo) ([]byte, int16, bool) {
	var frame UARTFrame
	payload, err := json.Marshal(&content.Content)
	if err != nil {
		return nil, 0, false
	}
	// payload, err := serialJson(content)
	// if err != nil {
	// 	return nil, 0, false
	// }

	frame.Header = headerConst
	frame.Cmd = byte(content.Cmd)
	frame.Payload = payload
	frame.Lenght = int16(len(payload) + 1) // 1 = size(cmd)
	// caculate CRC
	frame.CRC = headerConst + byte((frame.Lenght >> 8)) + byte((frame.Lenght & 0x00FF)) + frame.Cmd
	for _, b := range payload {
		frame.CRC += b
	}

	hightLenght := byte(frame.Lenght >> 8)
	lowLenght := byte(frame.Lenght & 0x00FF)
	bFrame := make([]byte, 0, frame.Lenght+4) // 4 = Header + Lenght + CRC

	bFrame = append(bFrame, frame.Header)
	bFrame = append(bFrame, hightLenght)
	bFrame = append(bFrame, lowLenght)
	bFrame = append(bFrame, frame.Cmd)
	bFrame = append(bFrame, frame.Payload...)
	bFrame = append(bFrame, frame.CRC)
	return bFrame, (frame.Lenght + 4), true
}

type labelsType []string

func (l labelsType) getType() string {
	for _, s := range l {
		if (s == DEVICETYPE) || (s == GROUPTYPE) || (s == SCENARIOTYPE) {
			return s
		}
	}
	return ""
}

func (l labelsType) isInitializied() bool {
	if l == nil {
		return false
	}
	for _, s := range l {
		if s == INITIALIZIED {
			return true
		}
	}
	return false
}

func (l labelsType) setInitializied() {
	for i, label := range l {
		if label == INITIALIZIED {
			return
		}
		if label == UNINITIALIZIED {
			l[i] = INITIALIZIED
			return
		}
	}
	l = append(l, INITIALIZIED)
	return
}
