//go:build integration
// +build integration

package main

import (
	"os"
	"os/exec"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
)

func testDestroy(c *qt.C, dbName string) {
	output, err := turso("db", "destroy", "--yes", dbName)
	c.Assert(err, qt.IsNil, qt.Commentf(output))
	c.Assert(output, qt.Contains, "Destroyed database "+dbName)
}

func testCreate(c *qt.C, dbName string, region *string, canary bool) {
	args := []string{"db", "create", dbName}
	if region != nil {
		args = append(args, "--region", *region)
	}
	if canary {
		args = append(args, "--canary")
	}
	output, err := turso(args...)
	defer testDestroy(c, dbName)
	c.Assert(err, qt.IsNil, qt.Commentf(output))
	c.Assert(output, qt.Contains, "Created database "+dbName)
}

func TestDbCreation(t *testing.T) {
	c := qt.New(t)
	for _, canary := range []bool{false, true} {
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			testCreate(c, "t1", nil, canary)
		}()
		for _, region := range []string{"waw", "gru", "sea"} {
			go func(region string, canary bool) {
				defer wg.Done()
				testCreate(c, "t1-"+region, &region, canary)
			}(region, canary)
		}
		wg.Wait()
	}
}

func turso(args ...string) (string, error) {
	cmd := exec.Command("../cmd/turso/turso", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
