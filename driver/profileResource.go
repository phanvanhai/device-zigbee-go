package driver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdk "github.com/edgexfoundry/device-sdk-go"
	dsModels "github.com/edgexfoundry/device-sdk-go/pkg/models"
	sdkModel "github.com/edgexfoundry/device-sdk-go/pkg/models"
	"github.com/edgexfoundry/go-mod-core-contracts/models"
	contract "github.com/edgexfoundry/go-mod-core-contracts/models"
)

const (
	getCmdMethod string = "get"
	setCmdMethod string = "set"
)

func profileResourceSliceToMaps(profileResources []contract.ProfileResource) (map[string][]contract.ResourceOperation, map[string][]contract.ResourceOperation) {
	getResult := make(map[string][]contract.ResourceOperation, len(profileResources))
	setResult := make(map[string][]contract.ResourceOperation, len(profileResources))
	for _, pr := range profileResources {
		if len(pr.Get) > 0 {
			getResult[pr.Name] = pr.Get
		}
		if len(pr.Set) > 0 {
			setResult[pr.Name] = pr.Set
		}
	}
	return getResult, setResult
}

func execWriteCmd(d *Driver, device contract.Device, cmd string, params string) ([]sdkModel.CommandRequest, []*sdkModel.CommandValue, error) {
	ros, err := getResourceOperationsByCommand(device, device.Profile.Name, cmd, setCmdMethod)
	if err != nil {
		msg := fmt.Sprintf("Handler - execWriteCmd: can't find ResrouceOperations in Profile(%s) and Command(%s), %v", device.Profile.Name, cmd, err)
		driver.Logger.Error(msg)
		return nil, nil, err
	}

	service := sdk.RunningService()

	cvs, err := parseWriteParams(d, device, device.Profile.Name, ros, params)
	if err != nil {
		msg := fmt.Sprintf("Handler - execWriteCmd: Put parameters parsing failed: %s", params)
		driver.Logger.Error(msg)
		return nil, nil, err
	}

	reqs := make([]dsModels.CommandRequest, len(cvs))
	for i, cv := range cvs {
		drName := cv.DeviceResourceName
		driver.Logger.Debug(fmt.Sprintf("Handler - execWriteCmd: putting deviceResource: %s", drName))

		// TODO: add recursive support for resource command chaining. This occurs when a
		// deviceprofile resource command operation references another resource command
		// instead of a device resource (see BoschXDK for reference).

		dr, ok := service.DeviceResource(device.Name, drName, setCmdMethod)

		driver.Logger.Debug(fmt.Sprintf("Handler - execWriteCmd: putting deviceResource: %s", drName))
		if !ok {
			msg := fmt.Sprintf("Handler - execWriteCmd: no deviceResource: %s for dev: %s cmd: %s method: GET", drName, device.Name, cmd)
			driver.Logger.Error(msg)
			return nil, nil, fmt.Errorf(msg)
		}

		reqs[i].DeviceResourceName = cv.DeviceResourceName
		reqs[i].Attributes = dr.Attributes
		reqs[i].Type = cv.Type
	}

	return reqs, cvs, nil
}

func getResourceOperationsByCommand(device contract.Device, profileName string, cmd string, method string) ([]models.ResourceOperation, error) {
	service := sdk.RunningService()
	sliceProfile := service.DeviceProfiles()
	for _, pro := range sliceProfile {
		if pro.Name == profileName {
			mapGet, mapSet := profileResourceSliceToMaps(pro.DeviceCommands)
			if strings.ToLower(method) == getCmdMethod {
				resOps, ok := mapGet[cmd]
				if !ok {
					return nil, fmt.Errorf("specified cmd: %s not found", cmd)
				}
				return resOps, nil
			} else if strings.ToLower(method) == setCmdMethod {
				resOps, ok := mapSet[cmd]
				if !ok {
					return nil, fmt.Errorf("specified cmd: %s not found", cmd)
				}
				return resOps, nil
			}
			return nil, fmt.Errorf("specified method: %s not found", method)
		}
	}
	return nil, fmt.Errorf("khong tim thay profile:" + profileName)
}

func parseWriteParams(d *Driver, device contract.Device, profileName string, ros []contract.ResourceOperation, params string) ([]*dsModels.CommandValue, error) {
	paramMap, err := parseParams(d, params)
	if err != nil {
		return []*dsModels.CommandValue{}, err
	}

	service := sdk.RunningService()

	result := make([]*dsModels.CommandValue, 0, len(paramMap))
	for _, ro := range ros {
		driver.Logger.Debug(fmt.Sprintf("looking for %s in the request parameters", ro.DeviceResource))
		p, ok := paramMap[ro.DeviceResource]
		if !ok {
			dr, ok := service.DeviceResource(device.Name, ro.DeviceResource, setCmdMethod)
			if !ok {
				err := fmt.Errorf("the parameter %s does not match any DeviceResource in DeviceProfile", ro.DeviceResource)
				return []*dsModels.CommandValue{}, err
			}

			if ro.Parameter != "" {
				driver.Logger.Debug(fmt.Sprintf("there is no %s in the request parameters, retrieving value from the Parameter field from the ResourceOperation", ro.DeviceResource))
				p = ro.Parameter
			} else if dr.Properties.Value.DefaultValue != "" {
				driver.Logger.Debug(fmt.Sprintf("there is no %s in the request parameters, retrieving value from the DefaultValue field from the ValueProperty", ro.DeviceResource))
				p = dr.Properties.Value.DefaultValue
			} else {
				err := fmt.Errorf("the parameter %s is not defined in the request body and there is no default value", ro.DeviceResource)
				return []*dsModels.CommandValue{}, err
			}
		}

		if len(ro.Mappings) > 0 {
			newP, ok := ro.Mappings[p]
			if ok {
				p = newP
			} else {
				msg := fmt.Sprintf("parseWriteParams: Resource (%s) mapping value (%s) failed with the mapping table: %v", ro.DeviceResource, p, ro.Mappings)
				driver.Logger.Warn(msg)
				//return result, fmt.Errorf(msg) // issue #89 will discuss how to handle there is no mapping matched
			}
		}

		cv, err := createCommandValueFromRO(d, device, profileName, &ro, p)
		if err == nil {
			result = append(result, cv)
		} else {
			return result, err
		}
	}

	return result, nil
}

func parseParams(d *Driver, params string) (paramMap map[string]string, err error) {
	err = json.Unmarshal([]byte(params), &paramMap)
	if err != nil {
		driver.Logger.Error(fmt.Sprintf("parsing Write parameters failed %s, %v", params, err))
		return
	}

	if len(paramMap) == 0 {
		err = fmt.Errorf("no parameters specified")
		return
	}
	return
}

func createCommandValueFromRO(d *Driver, device contract.Device, profileName string, ro *contract.ResourceOperation, v string) (*dsModels.CommandValue, error) {
	service := sdk.RunningService()
	dr, ok := service.DeviceResource(device.Name, ro.DeviceResource, setCmdMethod)
	if !ok {
		msg := fmt.Sprintf("createCommandValueForParam: no deviceResource: %s", ro.DeviceResource)
		driver.Logger.Error(msg)
		return nil, fmt.Errorf(msg)
	}

	return createCommandValueFromDR(d, &dr, v)
}

func createCommandValueFromDR(d *Driver, dr *contract.DeviceResource, v string) (*dsModels.CommandValue, error) {
	var result *dsModels.CommandValue
	var err error
	var value interface{}
	var t dsModels.ValueType
	origin := time.Now().UnixNano()

	switch strings.ToLower(dr.Properties.Value.Type) {
	case "bool":
		value, err = strconv.ParseBool(v)
		t = dsModels.Bool
	case "string":
		value = v
		t = dsModels.String
	case "uint8":
		n, e := strconv.ParseUint(v, 10, 8)
		value = uint8(n)
		err = e
		t = dsModels.Uint8
	case "uint16":
		n, e := strconv.ParseUint(v, 10, 16)
		value = uint16(n)
		err = e
		t = dsModels.Uint16
	case "uint32":
		n, e := strconv.ParseUint(v, 10, 32)
		value = uint32(n)
		err = e
		t = dsModels.Uint32
	case "uint64":
		value, err = strconv.ParseUint(v, 10, 64)
		t = dsModels.Uint64
	case "int8":
		n, e := strconv.ParseInt(v, 10, 8)
		value = int8(n)
		err = e
		t = dsModels.Int8
	case "int16":
		n, e := strconv.ParseInt(v, 10, 16)
		value = int16(n)
		err = e
		t = dsModels.Int16
	case "int32":
		n, e := strconv.ParseInt(v, 10, 32)
		value = int32(n)
		err = e
		t = dsModels.Int32
	case "int64":
		value, err = strconv.ParseInt(v, 10, 64)
		t = dsModels.Int64
	case "float32":
		n, e := strconv.ParseFloat(v, 32)
		value = float32(n)
		err = e
		t = dsModels.Float32
	case "float64":
		value, err = strconv.ParseFloat(v, 64)
		t = dsModels.Float64
	}

	if err != nil {
		driver.Logger.Error(fmt.Sprintf("Handler - Command: Parsing parameter value (%s) to %s failed: %v", v, dr.Properties.Value.Type, err))
		return result, err
	}

	result, err = dsModels.NewCommandValue(dr.Name, origin, value, t)

	return result, err
}
