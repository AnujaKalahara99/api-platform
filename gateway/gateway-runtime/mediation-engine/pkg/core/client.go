// Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package core

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func GenerateClientID(r *http.Request) string {
	// Check for X-WSO2-Client-ID header first
	if clientID := r.Header.Get("X-WSO2-Client-ID"); clientID != "" {
		return clientID
	}

	remoteAddr := r.RemoteAddr
	if remoteAddr == "" {
		return uuid.New().String()
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	if strings.Contains(host, ":") {
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		}
	}

	hash := sha256.Sum256([]byte(host))
	return hex.EncodeToString(hash[:])[:12]
}
