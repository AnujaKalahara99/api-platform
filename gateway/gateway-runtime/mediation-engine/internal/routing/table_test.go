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
    "testing"

    "github.com/wso2/api-platform/gateway/gateway-runtime/mediation-engine/pkg/core"
)

func TestTableAddAndLookup(t *testing.T) {
    table := NewTable()
    route := &core.Route{Source: "ws-in", Target: "kafka-main", ChannelSize: 1}
    table.Add(route)

    got, ok := table.Lookup("ws-in")
    if !ok {
        t.Fatal("expected route to be found")
    }
    if got.Target != "kafka-main" {
        t.Fatalf("expected target kafka-main, got %s", got.Target)
    }
}

func TestTableLookupMiss(t *testing.T) {
    table := NewTable()
    _, ok := table.Lookup("nonexistent")
    if ok {
        t.Fatal("expected route not to be found")
    }
}

func TestTableRemove(t *testing.T) {
    table := NewTable()
    table.Add(&core.Route{Source: "ws-in", Target: "kafka-main"})
    table.Remove("ws-in")

    _, ok := table.Lookup("ws-in")
    if ok {
        t.Fatal("expected route to be removed")
    }
}

func TestTableReplaceAll(t *testing.T) {
    table := NewTable()
    table.Add(&core.Route{Source: "old-route", Target: "old-target"})

    newRoutes := []*core.Route{
        {Source: "new-a", Target: "target-a"},
        {Source: "new-b", Target: "target-b"},
    }
    table.ReplaceAll(newRoutes)

    if _, ok := table.Lookup("old-route"); ok {
        t.Fatal("expected old route to be removed")
    }
    if _, ok := table.Lookup("new-a"); !ok {
        t.Fatal("expected new-a route to exist")
    }
    if _, ok := table.Lookup("new-b"); !ok {
        t.Fatal("expected new-b route to exist")
    }
}

func TestTableConcurrentAccess(t *testing.T) {
    table := NewTable()
    var wg sync.WaitGroup

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            route := &core.Route{Source: "src", Target: "tgt", ChannelSize: n}
            table.Add(route)
            table.Lookup("src")
        }(i)
    }
    wg.Wait()
}