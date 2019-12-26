// -*- Mode: Go; indent-tabs-mode: t -*-
//
// Copyright (C) 2018-2019 IOTech Ltd
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	device_zigbee "github.com/device-zigbee"
	"github.com/device-zigbee/driver"
	"github.com/edgexfoundry/device-sdk-go/pkg/startup"
)

const (
	serviceName string = "device-zigbee"
)

func main() {
	d := driver.NewProtocolDriver()
	startup.Bootstrap(serviceName, device_zigbee.Version, d)
}
