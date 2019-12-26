// -*- Mode: Go; indent-tabs-mode: t -*-
//
// Copyright (C) 2019 IOTech Ltd
//
// SPDX-License-Identifier: Apache-2.0

package driver

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/device-zigbee/driver/packet"

	sdk "github.com/edgexfoundry/device-sdk-go"
	sdkModel "github.com/edgexfoundry/device-sdk-go/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/models"
	"github.com/spf13/cast"
)

var once sync.Once
var driver *Driver

// Driver : Driver cua DS
type Driver struct {
	Logger  logger.LoggingClient
	AsyncCh chan<- *sdkModel.AsyncValues
}

// NewProtocolDriver : khoi tao driver, duoc goi trong ham main()
func NewProtocolDriver() sdkModel.ProtocolDriver {
	once.Do(func() {
		driver = new(Driver)
	})
	return driver
}

// Initialize performs protocol-specific initialization for the device
// service. The given *AsyncValues channel can be used to push asynchronous
// events and readings to Core Data.
func (d *Driver) Initialize(lc logger.LoggingClient, asyncCh chan<- *sdkModel.AsyncValues) error {
	d.Logger = lc
	d.AsyncCh = asyncCh
	Cache()
	packet.Repo()
	driver := sdk.DriverConfigs()
	port, ok := driver["SerialPort"]
	if !ok {
		return fmt.Errorf("Khong chi dinh SerialPort")
	}
	err := TransceiverInit(port)

	return err
}

// command of Master: {
//	"ManagerObjectName"
//	"ManagerCommandName"
//	"ManagerMethod"
//	"ManagerBody"
// }
// virtual manager DS
const (
	managerSubcribe     = "Subscribe"
	mangerSchedule      = "Schedule"
	managerRemoveItself = "RemoveItself"
	managerPutMethod    = "PUT"
	managerDeleteMethod = "DELETE"
)

var managerSubcribeAttInfo = AttributeInfo{
	ProfileID:   260,
	ClusterID:   64528,
	AttributeID: 17,
	ValueType:   0,
}
var mangerScheduleAttInfo = AttributeInfo{
	ProfileID:   260,
	ClusterID:   64528,
	AttributeID: 18,
	ValueType:   0,
}

type action struct {
	Command string `json:"command,omitempty"`
	Body    string `json:"body,omitempty"`
}
type contentElementType struct {
	OwnerID    string `json:"ownerID,omitempty"`
	ObjectType string `json:"type,omitempty"`
	ElementID  string `json:"elementID,omitempty"`
	action
}

// SubscribeStructZigbee : in Value of CommandFrame
type SubscribeStructZigbee struct {
	ObjectAddress // owerAddr
	AttributeValue
}

type contentScheduleType struct {
	OwnerID      string `json:"ownerID,omitempty"`
	ScheduleName string `json:"name,omitempty"`
	Time         int32  `json:"time,omitempty"`
	action
}

// ScheduleStructZigbee :
type ScheduleStructZigbee struct {
	ObjectAddress
	Name       string `json:"name"` // maxLen = 16
	DateHoMuSe int32  `json:"sche"` // date: bit 7 == isRepeat bit0->6 <-> Sun->Sat, = 0x0000 -> = Delete
	AttributeValue
}

// HandleReadCommands passes a slice of CommandRequest struct each representing
// a ResourceOperation for a specific device resource.
func (d *Driver) HandleReadCommands(deviceName string, protocols map[string]models.ProtocolProperties, reqs []sdkModel.CommandRequest) ([]*sdkModel.CommandValue, error) {
	var responses = make([]*sdkModel.CommandValue, len(reqs))
	var err error

	for i, req := range reqs {
		res, err := d.handleReadCommandRequest(deviceName, req)
		if err != nil {
			driver.Logger.Info(fmt.Sprintf("Handle read commands failed: %v", err))
			return responses, err
		}
		responses[i] = res
	}

	return responses, err
}

func (d *Driver) handleReadCommandRequest(objectName string, req sdkModel.CommandRequest) (*sdkModel.CommandValue, error) {
	var result = &sdkModel.CommandValue{}
	var err error

	idObject, ok := Cache().ConvertNameToIDObject(objectName)
	if !ok {
		return result, fmt.Errorf("Khong ton tai doi tuong")
	}

	objectInfo, ok := Cache().ConvertIDToObjectInfo(idObject)
	if !ok {
		return result, fmt.Errorf("Khong co thong tin dia chi doi tuong")
	}
	commandID := int8(CommandIDRead)
	attInfo, ok := Cache().ConvertResToAtt(req.DeviceResourceName)
	if !ok {
		return result, fmt.Errorf("Khong the chuyen doi Resource sang Attribute Zigbee")
	}

	cmFrame := CommandFrame{
		ObjectAddress: objectInfo.ObjectAddress,
		CommandID:     commandID,
		AttributeInfo: attInfo,
	}
	// crate TX_frame
	contentRepo := ContentRepo{
		Cmd:     CommandCmdConst,
		Content: cmFrame,
	}

	_, err = SendUartPacket(contentRepo, 5000)
	if err != nil {
		driver.Logger.Error(err.Error())
		return result, err
	}
	driver.Logger.Info(fmt.Sprintf("Send command: %+v", contentRepo))

	nameRepo := packet.Repo().GetRepoNameByID(idObject)
	responseRaw, ok := packet.Repo().GetFromRepoAfterResetWithTime(nameRepo, 100, 50)
	if !ok {
		return result, fmt.Errorf("Loi nhan phan hoi")
	}

	driver.Logger.Info(fmt.Sprintf("Parse command response: %+v", responseRaw))
	respByte, _ := json.Marshal(responseRaw)
	var responseRepo packet.ContentRepoStruct
	err = json.Unmarshal(respByte, &responseRepo)
	if err != nil {
		return result, fmt.Errorf("Loi phan tich phan hoi")
	}

	respByte, _ = json.Marshal(responseRepo.Packet)
	var response ResponseCommonFrame

	err = json.Unmarshal(respByte, &response)
	if err != nil {
		return result, fmt.Errorf("Loi phan tich phan hoi")
	}

	statusResponse := response.StatusResponse

	if statusResponse != 0 {
		return result, fmt.Errorf("Lenh gui toi Device Zigbee khong thanh cong")
	}

	reading := response.Value
	result, err = newResult(req, reading)
	if err != nil {
		return result, err
	}
	driver.Logger.Info(fmt.Sprintf("Get command finished: %+v", result))

	return result, err
}

func getBytes(data interface{}) ([]byte, error) {

	switch v := data.(type) {
	case int:
		nv := int32(v)
		buf := new(bytes.Buffer)
		err := binary.Write(buf, binary.BigEndian, nv)
		if err != nil {
			fmt.Println("binary.Write failed:", err)
		}
		return buf.Bytes(), nil
	case string:
		return []byte(v), nil
	case bool:
		r := make([]byte, 1)
		if v == true {
			r[0] = 1
		} else {
			r[0] = 0
		}
		return r, nil
	case int8:
		r := make([]byte, 1)
		r[0] = byte(v)
		return r, nil
	case uint8:
		r := make([]byte, 1)
		r[0] = byte(v)
		return r, nil
	default:
		buf := new(bytes.Buffer)
		err := binary.Write(buf, binary.BigEndian, v)
		if err != nil {
			fmt.Println("binary.Write failed:", err)
		}
		return buf.Bytes(), nil
	}
}

func convertScheduleStructZigbeeToBinary(from ScheduleStructZigbee) []byte {
	result := make([]byte, (2 + 1 + 1 + 18 + 4 + 2 + 2 + 2 + 1 + 8))
	var bValue []byte

	bValue, _ = getBytes(from.Value)
	dValue, _ := getBytes(from.DateHoMuSe)

	result[0] = byte(from.Address >> 8)
	result[1] = byte(from.Address & 0x00FF)
	result[2] = byte(from.Type)
	result[3] = byte(from.Endpoint)

	// result = append(result, ([]byte(from.Name))...)
	copy(result[4:], ([]byte(from.Name)))
	copy(result[22:], dValue)

	result[26] = byte(from.ProfileID >> 8)
	result[27] = byte(from.ProfileID & 0x00FF)
	result[28] = byte(from.ClusterID >> 8)
	result[29] = byte(from.ClusterID & 0x00FF)
	result[30] = byte(from.AttributeID >> 8)
	result[31] = byte(from.AttributeID & 0x00FF)
	result[32] = byte(from.ValueType)
	copy(result[33:], bValue)
	return result
}

func convertSubscribeStructZigbeeToBinary(from SubscribeStructZigbee, attNil bool) []byte {
	if attNil {
		result := make([]byte, (2 + 1 + 1))
		result[0] = byte(from.Address >> 8)
		result[1] = byte(from.Address & 0x00FF)
		result[2] = byte(from.Type)
		result[3] = byte(from.Endpoint)
		return result
	}
	result := make([]byte, (2 + 1 + 1 + 2 + 2 + 2 + 1 + 8))
	var bValue []byte

	bValue, _ = getBytes(from.Value)

	result[0] = byte(from.Address >> 8)
	result[1] = byte(from.Address & 0x00FF)
	result[2] = byte(from.Type)
	result[3] = byte(from.Endpoint)
	result[4] = byte(from.ProfileID >> 8)
	result[5] = byte(from.ProfileID & 0x00FF)
	result[6] = byte(from.ClusterID >> 8)
	result[7] = byte(from.ClusterID & 0x00FF)
	result[8] = byte(from.AttributeID >> 8)
	result[9] = byte(from.AttributeID & 0x00FF)
	result[10] = byte(from.ValueType)
	copy(result[11:], bValue)
	return result
}

func convertByteToUint8(in []byte) []uint16 {
	out := make([]uint16, len(in))
	for i, b := range in {
		out[i] = uint16(b)
	}
	return out
}

func (d *Driver) handleMasterRequest(reqs []sdkModel.CommandRequest, params []*sdkModel.CommandValue) error {
	if len(reqs) != 4 {
		driver.Logger.Info("Yeu cau khong hop le")
		return fmt.Errorf("Yeu cau khong hop le")
	}
	objectName, err := params[0].StringValue()
	if err != nil {
		return err
	}
	objectID, ok := Cache().ConvertNameToIDObject(objectName)
	if !ok {
		driver.Logger.Info("Khong ton tai doi tuong")
		return fmt.Errorf("Khong ton tai doi tuong")
	}
	objectInfo, ok := Cache().ConvertIDToObjectInfo(objectID)
	if !ok {
		driver.Logger.Info("Khong co thong tin dia chi doi tuong")
		return fmt.Errorf("Khong co thong tin dia chi doi tuong")
	}

	cmName, err := params[1].StringValue()
	if err != nil {
		return err
	}

	method, err := params[2].StringValue()
	if err != nil {
		return err
	}
	var commandID int8
	if method == managerPutMethod {
		commandID = CommandIDWrite
	} else if method == managerDeleteMethod {
		commandID = CommandIDDelete
	} else {
		driver.Logger.Info("Khong ho tro Method:" + method)
		return fmt.Errorf("Khong ho tro Method:" + method)
	}

	body, err := params[3].StringValue()
	if err != nil {
		return err
	}

	service := sdk.RunningService()

	// deviceObject, ok := service.DeviceResource(deviceName, cmd, "get")
	var cmFrame CommandFrame

	switch cmName {
	case managerSubcribe:
		var content contentElementType
		json.Unmarshal([]byte(body), &content)

		object, err := service.GetDeviceByName(objectName)
		if err != nil {
			return err
		}

		addrInfoOwer, ok := Cache().ConvertIDToObjectInfo(content.OwnerID)
		if !ok {
			driver.Logger.Info("Khong ton tai doi tuong:" + content.OwnerID)
			return fmt.Errorf("Khong ton tai doi tuong:" + content.OwnerID)
		}

		var newreqs []sdkModel.CommandRequest
		var newparams []*sdkModel.CommandValue
		var attvl = AttributeValue{}
		var attvlNil = true

		ownerName, ok := Cache().ConvertIDToNameObject(content.OwnerID)
		if !ok {
			driver.Logger.Info("Khong ton tai doi tuong")
			return fmt.Errorf("Khong ton tai doi tuong")
		}
		objectOwner, err := service.GetDeviceByName(ownerName)
		if err != nil {
			return err
		}

		if labelsType(objectOwner.Labels).getType() == SCENARIOTYPE {
			attvlNil = false
			fmt.Println("driver 399: vao scenario")
			newreqs, newparams, err = execWriteCmd(d, object, content.Command, content.Body)
			if err != nil {
				driver.Logger.Info(fmt.Sprintf("chuyen doi lenh loi: %v", err))
				return err
			}
			v, e := newparams[0].Int8Value()
			fmt.Printf("driver 401: Int8Value=%d-%v\n", v, e)
			// hien tai chi ho tro 1 command - value
			att, ok := Cache().ConvertResToAtt(newreqs[0].DeviceResourceName)
			if !ok {
				driver.Logger.Info("Khong tim thay Attribute Zigbee cho:" + objectName)
				return fmt.Errorf("Khong tim thay Attribute Zigbee cho:" + objectName)
			}
			attValue, err := newCommandValue(newreqs[0].Type, newparams[0])
			fmt.Printf("driver 409:%+v\n", attValue)
			if err != nil {
				driver.Logger.Info("Doc gia tri Command Value loi")
				return fmt.Errorf("Doc gia tri Command Value loi")
			}
			attvl = AttributeValue{
				AttributeInfo: att,
				Value:         attValue,
			}
		}
		// else: attvl = AttributeValue{}

		var valTypeSubscribe = SubscribeStructZigbee{
			ObjectAddress:  addrInfoOwer.ObjectAddress,
			AttributeValue: attvl,
		}

		// attvlByte, err := json.Marshal(valTypeSubscribe)
		// if err != nil {
		// 	driver.Logger.Info(fmt.Sprintf("Loi phan tich Json:%v", err))
		// 	return err
		// }
		attvlByte := convertSubscribeStructZigbeeToBinary(valTypeSubscribe, attvlNil)
		driver.Logger.Info(fmt.Sprintf("attvlByte:%v", attvlByte))
		cmFrame = CommandFrame{
			ObjectAddress: objectInfo.ObjectAddress,
			CommandID:     commandID,
			AttributeInfo: managerSubcribeAttInfo,
			Value:         attvlByte,
		}
	case mangerSchedule:
		var content contentScheduleType
		json.Unmarshal([]byte(body), &content)

		object, err := service.GetDeviceByName(objectName)
		if err != nil {
			return err
		}

		addrInfoOwer, ok := Cache().ConvertIDToObjectInfo(content.OwnerID)
		if !ok {
			driver.Logger.Info("Khong ton tai doi tuong:" + content.OwnerID)
			return fmt.Errorf("Khong ton tai doi tuong:" + content.OwnerID)
		}

		var newreqs []sdkModel.CommandRequest
		var newparams []*sdkModel.CommandValue
		var attvl = AttributeValue{}

		newreqs, newparams, err = execWriteCmd(d, object, content.Command, content.Body)
		if err != nil {
			driver.Logger.Info(fmt.Sprintf("chuyen doi lenh loi: %v", err))
			return err
		}

		// hien tai chi ho tro 1 command - value
		att, ok := Cache().ConvertResToAtt(newreqs[0].DeviceResourceName)
		if !ok {
			driver.Logger.Info("Khong tim thay Attribute Zigbee cho:" + objectName)
			return fmt.Errorf("Khong tim thay Attribute Zigbee cho:" + objectName)
		}
		attValue, err := newCommandValue(newreqs[0].Type, newparams[0])
		if err != nil {
			driver.Logger.Info("Doc gia tri Command Value loi")
			return fmt.Errorf("Doc gia tri Command Value loi")
		}
		// TODO: xac dinh loai gia tri cho Value
		// Hien tai co dinh type(Value) = uint8_t
		attvl = AttributeValue{
			AttributeInfo: att,
			Value:         attValue,
		}

		var valTypeSchedule = ScheduleStructZigbee{
			ObjectAddress:  addrInfoOwer.ObjectAddress,
			Name:           content.ScheduleName,
			DateHoMuSe:     content.Time,
			AttributeValue: attvl,
		}
		// attvlByte, err := json.Marshal(valTypeSchedule)
		// if err != nil {
		// 	driver.Logger.Info(fmt.Sprintf("Loi phan tich Json:%v", err))
		// 	return err
		// }
		attvlByte := convertScheduleStructZigbeeToBinary(valTypeSchedule)
		driver.Logger.Info(fmt.Sprintf("attvlByte:%v", attvlByte))

		cmFrame = CommandFrame{
			ObjectAddress: objectInfo.ObjectAddress,
			CommandID:     commandID,
			AttributeInfo: mangerScheduleAttInfo,
			Value:         attvlByte,
		}
	case managerRemoveItself:
		cmFrame = CommandFrame{
			ObjectAddress: objectInfo.ObjectAddress,
			CommandID:     commandID,
			AttributeInfo: managerSubcribeAttInfo,
			Value:         AttributeValue{},
		}
	default:
		return fmt.Errorf("Khong ho tro yeu cau:" + cmName)
	}
	// crate TX_frame
	contentRepo := ContentRepo{
		Cmd:     CommandCmdConst,
		Content: cmFrame,
	}
	nameRepo := packet.Repo().GetRepoNameByID(objectID)

	driver.Logger.Info(fmt.Sprintf("gui vao Repo: %s : contentRepo= %+v", nameRepo, contentRepo))

	_, err = SendUartPacket(contentRepo, 5000)
	if err != nil {
		driver.Logger.Error(err.Error())
		return err
	}
	driver.Logger.Info(fmt.Sprintf("Send command: %+v", contentRepo))

	responseRaw, ok := packet.Repo().GetFromRepoAfterResetWithTime(nameRepo, 100, 50)
	if !ok {
		driver.Logger.Info("Loi khong nhan duoc phan hoi")
		return fmt.Errorf("Loi khong nhan duoc phan hoi")
	}

	driver.Logger.Info(fmt.Sprintf("Parse command response: %+v", responseRaw))
	respByte, _ := json.Marshal(responseRaw)
	var responseRepo packet.ContentRepoStruct
	err = json.Unmarshal(respByte, &responseRepo)
	if err != nil {
		return fmt.Errorf("Loi phan tich phan hoi")
	}

	respByte, _ = json.Marshal(responseRepo.Packet)
	var response ResponseCommonFrame

	err = json.Unmarshal(respByte, &response)
	if err != nil {
		return fmt.Errorf("Loi phan tich phan hoi")
	}

	statusResponse := response.StatusResponse

	if statusResponse != 0 {
		return fmt.Errorf("Lenh gui toi Device Zigbee khong thanh cong")
	}

	driver.Logger.Info(fmt.Sprintf("Put command finished"))
	return nil
}

// HandleWriteCommands passes a slice of CommandRequest struct each representing
// a ResourceOperation for a specific device resource.
// Since the commands are actuation commands, params provide parameters for the individual
// command.
func (d *Driver) HandleWriteCommands(objectName string, protocols map[string]models.ProtocolProperties, reqs []sdkModel.CommandRequest, params []*sdkModel.CommandValue) error {
	var err error
	if Cache().GetMasterDeviceName() == objectName {
		return d.handleMasterRequest(reqs, params)
	}

	for i, req := range reqs {
		err = d.handleWriteCommandRequest(objectName, req, params[i])
		if err != nil {
			driver.Logger.Info(fmt.Sprintf("Handle write commands failed: %v", err))
			return err
		}
	}

	return err
}

func (d *Driver) handleWriteCommandRequest(objectName string, req sdkModel.CommandRequest, param *sdkModel.CommandValue) error {
	var err error

	idObject, ok := Cache().ConvertNameToIDObject(objectName)
	if !ok {
		return fmt.Errorf("Khong ton tai doi tuong")
	}

	objectInfo, ok := Cache().ConvertIDToObjectInfo(idObject)
	if !ok {
		return fmt.Errorf("Khong co thong tin dia chi doi tuong")
	}
	commandID := int8(CommandIDWrite)
	attInfo, ok := Cache().ConvertResToAtt(req.DeviceResourceName)
	if !ok {
		return fmt.Errorf("Khong the chuyen doi Resource sang Attribute Zigbee")
	}

	commandValue, err := newCommandValue(req.Type, param)
	if err != nil {
		return err
	}

	cmFrame := CommandFrame{
		ObjectAddress: objectInfo.ObjectAddress,
		CommandID:     commandID,
		AttributeInfo: attInfo,
		Value:         commandValue,
	}
	// crate TX_frame
	contentRepo := ContentRepo{
		Cmd:     CommandCmdConst,
		Content: cmFrame,
	}

	_, err = SendUartPacket(contentRepo, 5000)
	if err != nil {
		driver.Logger.Error(err.Error())
		return err
	}
	// driver.Logger.Info(fmt.Sprintf("Send command: %+v", contentRepo))

	nameRepo := packet.Repo().GetRepoNameByID(idObject)
	responseRaw, ok := packet.Repo().GetFromRepoAfterResetWithTime(nameRepo, 100, 50)
	if !ok {
		return fmt.Errorf("Loi nhan phan hoi")
	}

	driver.Logger.Info(fmt.Sprintf("Parse command response: %+v", responseRaw))
	respByte, _ := json.Marshal(responseRaw)
	var responseRepo packet.ContentRepoStruct
	err = json.Unmarshal(respByte, &responseRepo)
	if err != nil {
		return fmt.Errorf("Loi phan tich phan hoi")
	}

	respByte, _ = json.Marshal(responseRepo.Packet)
	var response ResponseCommonFrame

	err = json.Unmarshal(respByte, &response)
	if err != nil {
		return fmt.Errorf("Loi phan tich phan hoi")
	}

	statusResponse := response.StatusResponse

	if statusResponse != 0 {
		return fmt.Errorf("Lenh gui toi Device Zigbee khong thanh cong")
	}

	driver.Logger.Info(fmt.Sprintf("Put command finished"))
	return nil
}

func newResult(req sdkModel.CommandRequest, reading interface{}) (*sdkModel.CommandValue, error) {
	var result = &sdkModel.CommandValue{}
	var err error
	var resTime = time.Now().UnixNano()
	castError := "fail to parse %v reading, %v"

	if !checkValueInRange(req.Type, reading) {
		err = fmt.Errorf("parse reading fail. Reading %v is out of the value type(%v)'s range", reading, req.Type)
		driver.Logger.Error(err.Error())
		return result, err
	}

	switch req.Type {
	case sdkModel.Bool:
		val, err := cast.ToBoolE(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewBoolValue(req.DeviceResourceName, resTime, val)
	case sdkModel.String:
		val, err := cast.ToStringE(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result = sdkModel.NewStringValue(req.DeviceResourceName, resTime, val)
	case sdkModel.Uint8:
		val, err := cast.ToUint8E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewUint8Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Uint16:
		val, err := cast.ToUint16E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewUint16Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Uint32:
		val, err := cast.ToUint32E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewUint32Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Uint64:
		val, err := cast.ToUint64E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewUint64Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Int8:
		val, err := cast.ToInt8E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewInt8Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Int16:
		val, err := cast.ToInt16E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewInt16Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Int32:
		val, err := cast.ToInt32E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewInt32Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Int64:
		val, err := cast.ToInt64E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewInt64Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Float32:
		val, err := cast.ToFloat32E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewFloat32Value(req.DeviceResourceName, resTime, val)
	case sdkModel.Float64:
		val, err := cast.ToFloat64E(reading)
		if err != nil {
			return nil, fmt.Errorf(castError, req.DeviceResourceName, err)
		}
		result, err = sdkModel.NewFloat64Value(req.DeviceResourceName, resTime, val)
	default:
		err = fmt.Errorf("return result fail, none supported value type: %v", req.Type)
	}

	return result, err
}

func newCommandValue(valueType sdkModel.ValueType, param *sdkModel.CommandValue) (interface{}, error) {
	var commandValue interface{}
	var err error
	switch valueType {
	case sdkModel.Bool:
		commandValue, err = param.BoolValue()
	case sdkModel.String:
		commandValue, err = param.StringValue()
	case sdkModel.Uint8:
		commandValue, err = param.Uint8Value()
	case sdkModel.Uint16:
		commandValue, err = param.Uint16Value()
	case sdkModel.Uint32:
		commandValue, err = param.Uint32Value()
	case sdkModel.Uint64:
		commandValue, err = param.Uint64Value()
	case sdkModel.Int8:
		commandValue, err = param.Int8Value()
	case sdkModel.Int16:
		commandValue, err = param.Int16Value()
	case sdkModel.Int32:
		commandValue, err = param.Int32Value()
	case sdkModel.Int64:
		commandValue, err = param.Int64Value()
	case sdkModel.Float32:
		commandValue, err = param.Float32Value()
	case sdkModel.Float64:
		commandValue, err = param.Float64Value()
	default:
		err = fmt.Errorf("fail to convert param, none supported value type: %v", valueType)
	}

	return commandValue, err
}

// func (d *Driver) DisconnectDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
// 	d.Logger.Warn("Driver's DisconnectDevice function didn't implement")
// 	return nil
// }

// Stop instructs the protocol-specific DS code to shutdown gracefully, or
// if the force parameter is 'true', immediately. The driver is responsible
// for closing any in-use channels, including the channel used to send async
// readings (if supported).
func (d *Driver) Stop(force bool) error {
	d.Logger.Warn("Driver's Stop function didn't implement")
	TransceiverClose()
	return nil
}

// lable init:
const (
	INITIALIZIED   = "initializied"
	UNINITIALIZIED = "uninitializied"
)

func createProvisionObjectContentRepo(frame ProvisonFrame) (result ContentRepo, repoName string) {
	result.Cmd = AddObjectCmdConst
	result.Content = frame

	repoName = packet.Repo().GetRepoNameByCMD(AddObjectCmdConst)
	return
}

func createDeleteObjectContentRepo(addr ObjectAddress) (result ContentRepo, repoName string) {
	result.Cmd = DeleteObjectCmdConst
	result.Content = DeleteObjectFrame{
		ObjectAddress: addr,
	}
	repoName = packet.Repo().GetRepoNameByCMD(DeleteObjectCmdConst)
	return
}

const (
	// PROTOCOLSNETWORKNAME :
	PROTOCOLSNETWORKNAME = "Network"
)

// AddDevice is a callback function that is invoked
// when a new Device associated with this Device Service is added
func (d *Driver) AddDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	d.Logger.Debug(fmt.Sprintf("Device %s is added", deviceName))
	service := sdk.RunningService()
	device, err := service.GetDeviceByName(deviceName)
	if err != nil {
		return err
	}

	Cache().UpdateObject(device)

	var mac string
	var pan uint16
	if len(device.Protocols) > 0 {
		pp, ok := device.Protocols[PROTOCOLSNETWORKNAME]
		if !ok {
			driver.Logger.Error(fmt.Sprintf("Khong co thong tin vat ly cua thiet bi"))
			return fmt.Errorf("Khong co thong tin vat ly cua thiet bi")
		}
		strMac, _ := pp["MAC"]
		strPan, _ := pp["PAN"]
		// umac, _ := strconv.ParseUint(strMac, 10, 64)
		upan, _ := strconv.ParseUint(strPan, 10, 16)
		mac = strMac
		pan = uint16(upan)
	}
	if labelsType(device.Labels).isInitializied() == false {
		// provision
		var frame ProvisonFrame
		if labelsType(device.Labels).getType() == DEVICETYPE {

			frame = ProvisonFrame{
				AddressEUI64: AddressEUI64{
					MAC: mac,
					PAN: pan,
				},
				NameDevice: deviceName,
			}
		} else {
			frame = ProvisonFrame{
				AddressEUI64: AddressEUI64{
					MAC: "00000000",
					PAN: pan,
				},
				NameDevice: deviceName,
			}
		}

		repo, repoName := createProvisionObjectContentRepo(frame)
		_, err := SendUartPacket(repo, 8000) // gui trong 4s
		if err != nil {
			driver.Logger.Error(err.Error())
			return err
		}
		driver.Logger.Info(fmt.Sprintf("Send request add object: %+v", repo))

		responseRaw, ok := packet.Repo().GetFromRepoAfterResetWithTime(repoName, 20000, 1)
		if !ok {
			return fmt.Errorf("Loi nhan phan hoi")
		}

		driver.Logger.Info(fmt.Sprintf("Parse command response: %+v", responseRaw))
		respByte, _ := json.Marshal(responseRaw)
		var responseRepo packet.ContentRepoStruct
		err = json.Unmarshal(respByte, &responseRepo)
		if err != nil {
			return fmt.Errorf("Loi phan tich phan hoi")
		}

		respByte, _ = json.Marshal(responseRepo.Packet)
		var response ResponseCommonFrame

		err = json.Unmarshal(respByte, &response)
		if err != nil {
			return fmt.Errorf("Loi phan tich phan hoi")
		}
		if response.StatusResponse != 0x00 {
			driver.Logger.Info(fmt.Sprintf("Status response: %+v", response.StatusResponse))
			return fmt.Errorf("Yeu cau thuc hien khong thanh cong")
		}
		objectInfo := response.ObjectInfo
		nw, ok := device.Protocols[nameNetworkProtocol]
		if !ok {
			nw = make(map[string]string)
		}
		nw[nameMACProperty] = objectInfo.MAC
		nw[namePANProperty] = strconv.FormatUint(uint64(objectInfo.PAN), 10)
		nw[nameAddressProperty] = strconv.FormatUint(uint64(objectInfo.Address), 10)
		nw[nameEndpointProperty] = strconv.FormatUint(uint64(objectInfo.Endpoint), 10)
		nw[nameTypeProperty] = strconv.FormatUint(uint64(objectInfo.Type), 10)
		device.Protocols[nameNetworkProtocol] = nw

		if response.Description != "" {
			device.Description = response.Description
		}
		labelsType(device.Labels).setInitializied()
		service.UpdateDevice(device)
	}

	return nil
}

// UpdateDevice is a callback function that is invoked
// when a Device associated with this Device Service is updated
func (d *Driver) UpdateDevice(deviceName string, protocols map[string]models.ProtocolProperties, adminState models.AdminState) error {
	d.Logger.Info(fmt.Sprintf("Device %s is updated", deviceName))
	service := sdk.RunningService()
	device, err := service.GetDeviceByName(deviceName)
	if err == nil {
		Cache().UpdateObject(device)
	}
	return nil
}

// RemoveDevice is a callback function that is invoked
// when a Device associated with this Device Service is removed
func (d *Driver) RemoveDevice(deviceName string, protocols map[string]models.ProtocolProperties) error {
	d.Logger.Info(fmt.Sprintf("Device %s is removed", deviceName))
	Cache().DeleteObject(deviceName)
	return nil
}

// func GetDriver() *Driver {
// 	return driver
// }

// PushEventGoroutine : chay gorountine de Push Event
func PushEventGoroutine(data ResponseCommonFrame) {
	objectID, ok := Cache().ConvertAddrToIDObject(data.ObjectAddress)
	if !ok {
		return
	}
	objectName, ok := Cache().ConvertIDToNameObject(objectID)
	if !ok {
		return
	}
	resource, ok := Cache().ConvertAttToRes(data.AttributeInfo)
	if !ok {
		return
	}

	req := sdkModel.CommandRequest{
		DeviceResourceName: resource.Name,
		Type:               sdkModel.ParseValueType(resource.Properties.Value.Type),
	}
	result, err := newResult(req, data.Value)
	if err != nil {
		return
	}
	asyncValues := &sdkModel.AsyncValues{
		DeviceName:    objectName,
		CommandValues: []*sdkModel.CommandValue{result},
	}

	driver.AsyncCh <- asyncValues
	driver.Logger.Info(fmt.Sprintf(" Pushed Event of Object=%s - value=%+v", objectName, data.Value))
}
