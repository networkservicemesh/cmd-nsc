// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package config - contain environment variables used by nsc
package config

import (
	"net/url"
	"time"

	"github.com/pkg/errors"
)

// Config - configuration for cmd-nsmgr
type Config struct {
	Name             string        `default:"nsc" desc:"Name of Network Service Client"`
	ConnectTo        url.URL       `default:"unix:///var/lib/networkservicemesh/nsm.io.sock" desc:"url to connect to NSM" split_words:"true"`
	DialTimeout      time.Duration `default:"5s" desc:"timeout to dial NSMgr" split_words:"true"`
	RequestTimeout   time.Duration `default:"15s" desc:"timeout to request NSE" split_words:"true"`
	RetryTimeout     time.Duration `default:"0s" desc:"retry timeout" split_words:"true"`
	RetryInterval    time.Duration `default:"100ms" desc:"retry interval" split_words:"true"`
	MaxTokenLifetime time.Duration `default:"10m" desc:"maximum lifetime of tokens" split_words:"true"`

	Labels    []string `default:"" desc:"A list of client labels with format key1=val1,key2=val2, will be used a primary list for network services" split_words:"true"`
	Mechanism string   `default:"kernel" desc:"Default Mechanism to use, supported values: kernel, vfio" split_words:"true"`

	NetworkServices []url.URL `default:"" desc:"A list of Network Service Requests" split_words:"true"`
	LogLevel        string    `default:"INFO" desc:"Log level" split_words:"true"`
}

// IsValid - check if configuration is valid
func (c *Config) IsValid() error {
	if len(c.NetworkServices) == 0 {
		return errors.New("no network services are specified")
	}
	if c.Name == "" {
		return errors.New("no client name specified")
	}
	if c.ConnectTo.String() == "" {
		return errors.New("no NSMGr ConnectTO URL are specified")
	}
	return nil
}
