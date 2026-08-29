package configs

import (
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	natsAddress                        string
	registrationReqTimeoutMilliseconds int64
	maxRegistrationRetries             int8
	nodeIdDirPath                      string
	nodeIdFileName                     string
	grpcServerAddress                  string
	serfBindAddress                    string
	serfBindPort                       int
	osPollInterval                     time.Duration
}

func (c *Config) NatsAddress() string {
	return c.natsAddress
}

func (c *Config) RegistrationReqTimeoutMilliseconds() int64 {
	return c.registrationReqTimeoutMilliseconds
}

func (c *Config) MaxRegistrationRetries() int8 {
	return c.maxRegistrationRetries
}

func (c *Config) NodeIdDirPath() string {
	return c.nodeIdDirPath
}

func (c *Config) NodeIdFileName() string {
	return c.nodeIdFileName
}

func (c *Config) GrpcServerAddress() string {
	return c.grpcServerAddress
}

func (c *Config) SerfBindAddress() string {
	return c.serfBindAddress
}

func (c *Config) SerfBindPort() int {
	return c.serfBindPort
}

func (c *Config) OsPollInterval() time.Duration {
	return c.osPollInterval
}

const (
	defaultNodeIdDirPath = "/var/lib/c12s"
	defaultNodeIdFile    = "nodeid"
)

func NewFromEnv() (*Config, error) {
	registrationReqTimeoutMilliseconds, err := strconv.Atoi(os.Getenv("REGISTRATION_REQ_TIMEOUT_MILLISECONDS"))
	if err != nil {
		log.Println(err)
		registrationReqTimeoutMilliseconds = 5000
	}
	maxRegistrationRetries, err := strconv.Atoi(os.Getenv("MAX_REGISTRATION_RETRIES"))
	if err != nil {
		log.Println(err)
		maxRegistrationRetries = 5
	}
	serfBindPort, err := strconv.Atoi(os.Getenv("BIND_PORT"))
	if err != nil {
		log.Println(err)
		serfBindPort = 7946
	}
	nodeIdDirPath := os.Getenv("NODE_ID_DIR_PATH")
	if nodeIdDirPath == "" {
		nodeIdDirPath = defaultNodeIdDirPath
	}
	nodeIdFileName := os.Getenv("NODE_ID_FILE_NAME")
	if nodeIdFileName == "" {
		nodeIdFileName = defaultNodeIdFile
	}
	osPollInterval, err := time.ParseDuration(os.Getenv("OS_POLL_INTERVAL"))
	if err != nil {
		log.Println(err)
		osPollInterval = 30 * time.Second
	}
	return &Config{
		natsAddress:                        os.Getenv("NATS_ADDRESS"),
		registrationReqTimeoutMilliseconds: int64(registrationReqTimeoutMilliseconds),
		maxRegistrationRetries:             int8(maxRegistrationRetries),
		nodeIdDirPath:                      nodeIdDirPath,
		nodeIdFileName:                     nodeIdFileName,
		grpcServerAddress:                  os.Getenv("STAR_ADDRESS"),
		serfBindAddress:                    os.Getenv("BIND_ADDRESS"),
		serfBindPort:                       serfBindPort,
		osPollInterval:                     osPollInterval,
	}, nil
}
