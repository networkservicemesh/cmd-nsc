// Copyright (c) 2020 Doc.ai and/or its affiliates.
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
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/vfio"
)

var configMechanisms = map[string]string{
	"kernel": kernel.MECHANISM,
	"vfio":   vfio.MECHANISM,
}

// Config - configuration for cmd-nsmgr
type Config struct {
	Name             string        `default:"nsc" desc:"Name of Network Service Client"`
	ConnectTo        url.URL       `default:"unix:///var/lib/networkservicemesh/nsm.io.sock" desc:"url to connect to NSM" split_words:"true"`
	MaxTokenLifetime time.Duration `default:"24h" desc:"maximum lifetime of tokens" split_words:"true"`

	Routes    []string `default:"" desc:"A list of routes asked by client" split_words:"true"`
	Labels    []string `default:"" desc:"A list of client labels with format key1=val1,key2=val2, will be used a primary list for network services" split_words:"true"`
	Mechanism string   `default:"kernel" desc:"Default Mechanism to use, supported values: kernel, vfio" split_words:"true"`

	NetworkServices []NetworkServiceConfig `default:"" desc:"A list of Network Service Requests" split_words:"true"`
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

// NetworkServiceConfig - defines a network service request configuration
type NetworkServiceConfig struct {
	NetworkService string            `default:"" desc:"A name of network service" split_words:"true"`
	Path           []string          `default:"" desc:"An interfaceName or memif socket file name" split_words:"true"`
	Mechanism      string            `default:"" desc:"Mechanism used by client" split_words:"true"`
	Labels         map[string]string `default:"" desc:"A map of client labels" split_words:"true"`
}

// UnmarshalBinary - load network service using environment, parsing from URL.
func (cfg *NetworkServiceConfig) UnmarshalBinary(text []byte) error {
	u1, err := url.Parse(string(text))
	if err != nil {
		return err
	}

	cfg.Mechanism = u1.Scheme
	if cfg.Mechanism != "" {
		m, ok := configMechanisms[cfg.Mechanism]
		if !ok {
			return errors.Errorf("invalid mechanism specified %v. Supported: %v", cfg.Mechanism, configMechanisms)
		}
		cfg.Mechanism = m
	}

	cfg.NetworkService = u1.Hostname()

	for _, segm := range strings.Split(u1.Path, "/") {
		if segm != "" {
			cfg.Path = append(cfg.Path, segm)
		}
	}

	for k, v := range u1.Query() {
		if cfg.Labels == nil {
			cfg.Labels = map[string]string{}
		}
		cfg.Labels[k] = v[0]
	}

	if cfg.NetworkService == "" && len(cfg.Path) > 0 {
		cfg.NetworkService = cfg.Path[0]
		cfg.Path = cfg.Path[1:]
	}

	return nil
}

// IsValid - check if network service request is correct.
func (cfg *NetworkServiceConfig) IsValid() error {
	if cfg.NetworkService == "" {
		return errors.New("no network service specified")
	}
	switch cfg.Mechanism {
	case kernel.MECHANISM:
		// Verify interface name
		if len(cfg.Path) > 1 {
			return errors.Errorf("invalid client interface name specified: %s", strings.Join(cfg.Path, "/"))
		}
		if len(cfg.Path[0]) > 15 {
			return errors.Errorf("interface part cannot exceed 15 characters: %s", strings.Join(cfg.Path, "/"))
		}
	case vfio.MECHANISM:
		// There should be no path
		if len(cfg.Path) > 0 {
			return errors.Errorf("no path supported for the VFIO mechanism: %s", strings.Join(cfg.Path, "/"))
		}
	}
	return nil
}

// MergeWithConfigOptions - perform merge of config options with network service config options.
func (cfg *NetworkServiceConfig) MergeWithConfigOptions(conf *Config) error {
	// Update mechanism if not specified
	if cfg.Mechanism == "" && conf.Mechanism == "" {
		return errors.New("no mechanism specified")
	}

	if cfg.Mechanism == "" && conf.Mechanism != "" {
		m, ok := configMechanisms[conf.Mechanism]
		if !ok {
			return errors.Errorf("invalid mechanism specified %v. Supported: %v", conf.Mechanism, configMechanisms)
		}
		cfg.Mechanism = m
	}
	// Add labels from root config if not specified.
	for _, kv := range conf.Labels {
		keyValue := strings.Split(kv, "=")
		if len(keyValue) != 2 {
			keyValue = []string{"", ""}
		}
		key := strings.Trim(keyValue[0], " ")
		if _, ok := cfg.Labels[key]; !ok {
			if cfg.Labels == nil {
				cfg.Labels = map[string]string{}
			}
			cfg.Labels[key] = strings.Trim(keyValue[1], " ")
		}
	}
	return cfg.IsValid()
}
