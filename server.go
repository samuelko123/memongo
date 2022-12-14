package memongo

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/phayes/freeport"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Server struct {
	cmd    *exec.Cmd
	port   int
	client *mongo.Client
}

var (
	reReady                 = regexp.MustCompile(`waiting for connections.*port\D*(\d+)`)
	reAlreadyInUse          = regexp.MustCompile("addr already in use")
	reAlreadyRunning        = regexp.MustCompile("mongod already running")
	rePermissionDenied      = regexp.MustCompile("mongod permission denied")
	reDataDirectoryNotFound = regexp.MustCompile("data directory .*? not found")
	reShuttingDown          = regexp.MustCompile("shutting down with code")
)

func (s *Server) Start() error {
	var err error

	mongodPath := path.Join(os.Getenv("HOMEPATH"), ".mongod", "mongod.exe")
	_, err = os.Stat(mongodPath)
	if err != nil {
		return fmt.Errorf("mongod binary does not exist - %s", mongodPath)
	}

	dbDir, err := os.MkdirTemp(os.TempDir(), "")
	if err != nil {
		return errors.New("cannot create temp dir")
	}

	port, err := freeport.GetFreePort()
	if err != nil {
		return errors.New("cannot get new port")
	}

	args := []string{"--dbpath", dbDir, "--port", strconv.Itoa(port), "--storageEngine", "ephemeralForTest"}
	s.cmd = exec.Command(mongodPath, args...)

	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)

	err = s.cmd.Start()
	if err != nil {
		return err
	}

	// scan stdout to determine whether server is ready (or failed)
	for scanner.Scan() {
		line := scanner.Text()
		downcaseLine := strings.ToLower(line)
		if match := reReady.FindStringSubmatch(downcaseLine); match != nil {
			s.port, err = strconv.Atoi(match[1])
			if err != nil {
				return fmt.Errorf("cannot parse port from mongod log line: %s", downcaseLine)
			}

			// collect output from stdout
			// otherwise mongod will get stuck
			go func() {
				for scanner.Scan() {
					scanner.Text()
				}
			}()

			return nil
		} else if reAlreadyInUse.MatchString(downcaseLine) {
			return errors.New(downcaseLine)
		} else if reAlreadyRunning.MatchString(downcaseLine) {
			return errors.New(downcaseLine)
		} else if rePermissionDenied.MatchString(downcaseLine) {
			return errors.New(downcaseLine)
		} else if reDataDirectoryNotFound.MatchString(downcaseLine) {
			return errors.New(downcaseLine)
		} else if reShuttingDown.MatchString(downcaseLine) {
			return errors.New(downcaseLine)
		}
	}

	return errors.New("mongod exited before startup completed")
}

func (s *Server) NewDatabase() (*mongo.Database, error) {
	// name variables
	var err error
	ctx := context.Background()

	// connect to mongod
	uri := fmt.Sprintf("mongodb://localhost:%d", s.port)
	if s.client == nil {
		s.client, err = mongo.Connect(ctx, options.Client().ApplyURI(uri))
		if err != nil {
			return nil, errors.Wrapf(err, "cannot connect to mongodb server")
		}
	}

	// get new database
	dbName, err := generateDBName(15)
	if err != nil {
		return nil, err
	}
	db := s.client.Database(dbName)

	return db, nil
}

func (s *Server) Stop() {
	s.cmd.Process.Kill()
}

func (s *Server) Port() int {
	return s.port
}
