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

package routing

import (
	"sync"

	"github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

type Table struct {
	routes sync.Map
}

func NewTable() *Table {
	return &Table{}
}

func (t *Table) Add(route *core.Route) {
	t.routes.Store(route.Source, route)
}

func (t *Table) Remove(source string) {
	t.routes.Delete(source)
}

func (t *Table) Lookup(source string) (*core.Route, bool) {
	v, ok := t.routes.Load(source)
	if !ok {
		return nil, false
	}
	return v.(*core.Route), true
}

func (t *Table) ReplaceAll(routes []*core.Route) {
	t.routes.Range(func(key, _ any) bool {
		t.routes.Delete(key)
		return true
	})
	for _, r := range routes {
		t.routes.Store(r.Source, r)
	}
}
