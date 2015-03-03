package testutil

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
)

var offset uint64

type TestPortConfig struct {
	DNS     int `json:"dns,omitempty"`
	HTTP    int `json:"http,omitempty"`
	RPC     int `json:"rpc,omitempty"`
	SerfLan int `json:"serf_lan,omitempty"`
	SerfWan int `json:"serf_wan,omitempty"`
	Server  int `json:"server,omitempty"`
}

type TestAddressConfig struct {
	HTTP string `json:"http,omitempty"`
}

type TestServerConfig struct {
	Bootstrap bool               `json:"bootstrap,omitempty"`
	Server    bool               `json:"server,omitempty"`
	DataDir   string             `json:"data_dir,omitempty"`
	LogLevel  string             `json:"log_level,omitempty"`
	Addresses *TestAddressConfig `json:"addresses,omitempty"`
	Ports     *TestPortConfig    `json:"ports,omitempty"`
}

type ServerConfigCallback func(c *TestServerConfig)

func defaultServerConfig() *TestServerConfig {
	idx := int(atomic.AddUint64(&offset, 1))

	return &TestServerConfig{
		Bootstrap: true,
		Server:    true,
		LogLevel:  "debug",
		Ports: &TestPortConfig{
			DNS:     19000 + idx,
			HTTP:    18800 + idx,
			RPC:     18600 + idx,
			SerfLan: 18200 + idx,
			SerfWan: 18400 + idx,
			Server:  18000 + idx,
		},
	}
}

type TestServer struct {
	pid     int
	dataDir string
	config  *TestServerConfig
}

func NewTestServer(t *testing.T) *TestServer {
	return NewTestServerConfig(t, nil)
}

func NewTestServerConfig(t *testing.T, cb ServerConfigCallback) *TestServer {
	if path, err := exec.LookPath("consul"); err != nil || path == "" {
		t.Log("consul not found on $PATH, skipping")
		t.SkipNow()
	}

	dataDir, err := ioutil.TempDir("", "consul")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	configFile, err := ioutil.TempFile("", "consul")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Remove(configFile.Name())

	consulConfig := defaultServerConfig()
	consulConfig.DataDir = dataDir

	if cb != nil {
		cb(consulConfig)
	}

	configContent, err := json.Marshal(consulConfig)
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if _, err := configFile.Write(configContent); err != nil {
		t.Fatalf("err: %s", err)
	}
	configFile.Close()

	// Start the server
	cmd := exec.Command("consul", "agent", "-config-file", configFile.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	server := &TestServer{
		config:  consulConfig,
		pid:     cmd.Process.Pid,
		dataDir: dataDir,
	}

	if err := server.waitForLeader(); err != nil {
		t.Fatalf("err: %s", err)
	}
	return server
}

func (s *TestServer) Stop() {
	defer os.RemoveAll(s.dataDir)

	cmd := exec.Command("kill", "-9", fmt.Sprintf("%d", s.pid))
	if err := cmd.Run(); err != nil {
		panic(err)
	}
}

func (s *TestServer) waitForLeader() error {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/catalog/nodes", s.config.Ports.HTTP)

	WaitForResult(func() (bool, error) {
		resp, err := http.Get(url)
		if err != nil {
			return false, err
		}
		resp.Body.Close()

		// Ensure we have a leader and a node registeration
		if leader := resp.Header.Get("X-Consul-KnownLeader"); leader != "true" {
			fmt.Println(leader)
			return false, fmt.Errorf("Consul leader status: %#v", leader)
		}
		if resp.Header.Get("X-Consul-Index") == "0" {
			return false, fmt.Errorf("Consul index is 0")
		}

		return true, nil
	}, func(err error) {
		panic(err)
	})

	return nil
}
