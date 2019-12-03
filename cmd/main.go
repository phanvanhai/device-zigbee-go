// -*- Mode: Go; indent-tabs-mode: t -*-
//
// Copyright (C) 2018-2019 IOTech Ltd
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/edgexfoundry/device-sdk-go/pkg/startup"
	device "github.com/my-ds"
	"github.com/my-ds/driver"
)

const (
	serviceName string = "my-zigbee"
)

func main() {
	d := driver.NewProtocolDriver()
	startup.Bootstrap(serviceName, device.Version, d)
}
