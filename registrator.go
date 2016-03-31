package main

import (
	"errors"
	"os"
	"time"

	dockerapi "github.com/fsouza/go-dockerclient"
	"github.com/xytis/registrator/bridge"
	. "github.com/xytis/registrator/common"

	"github.com/jawher/mow.cli"
)

func assert(err error) {
	if err != nil {
		Log.Fatalln(err)
	}
}

func main() {
	app := cli.App("registrator", "Docker container registrator")
	func() {
		ver := Version
		rel := VersionPrerelease
		if GitDescribe != "" {
			ver = GitDescribe
		}
		if rel != "" {
			rel = "-" + rel
		}
		app.Version("v version", ver+rel)
	}()

	var (
		logLevel = app.String(cli.StringOpt{
			Name:   "log-level",
			Value:  "info",
			Desc:   "logging level (debug, info, warning, error)",
			EnvVar: "LOG_LEVEL",
		})
		hostIp = app.String(cli.StringOpt{
			Name:   "ip",
			Value:  "",
			Desc:   "IP for ports mapped to the host",
			EnvVar: "HOST_IP",
		})
		internal = app.Bool(cli.BoolOpt{
			Name:   "internal",
			Value:  false,
			Desc:   "Use internal ports instead of published ones",
			EnvVar: "PUBLISH_INTERNAL",
		})
		global = app.Bool(cli.BoolOpt{
			Name:   "global",
			Value:  false,
			Desc:   "Use container IP's as they are publicly available",
			EnvVar: "PUBLISH_GLOBAL",
		})
		refreshTtl = app.Int(cli.IntOpt{
			Name:   "ttl-refresh",
			Value:  0,
			Desc:   "Frequency with which service TTLs are refreshed",
			EnvVar: "REFRESH_TTL",
		})
		refreshInterval = app.Int(cli.IntOpt{
			Name:   "ttl",
			Value:  0,
			Desc:   "TTL for services (default is no expiry)",
			EnvVar: "REFRESH_INTERVAL",
		})
		resyncInterval = app.Int(cli.IntOpt{
			Name:   "resync",
			Value:  0,
			Desc:   "Frequency with which services are resynchronized",
			EnvVar: "RESYNC_INTERVAL",
		})
		retryAttempts = app.Int(cli.IntOpt{
			Name:   "retry-attempts",
			Value:  0,
			Desc:   "Max retry attempts to establish a connection with the backend. Use -1 for infinite retries",
			EnvVar: "RETRY_ATTEMPTS",
		})
		retryInterval = app.Int(cli.IntOpt{
			Name:   "retry-interval",
			Value:  2000,
			Desc:   "Interval (in millisecond) between retry-attempts.",
			EnvVar: "RETRY_INTERVAL",
		})
		forceTags  = app.StringOpt("tags", "", "Append tags for all registered services")
		deregister = app.StringOpt("deregister", "always", "Deregister exited services \"always\" or \"on-success\"")
		cleanup    = app.BoolOpt("cleanup", false, "Remove dangling services")
		registry   = app.StringArg("REGISTRY", "", "Registry url")
	)

	app.Action = func() {
		SetLogLevel(*logLevel)

		Log.Infof("Starting registrator %s ...", Version)

		if *hostIp != "" {
			Log.Infoln("Forcing host IP to", *hostIp)
		}

		if (*refreshTtl == 0 && *refreshInterval > 0) || (*refreshTtl > 0 && *refreshInterval == 0) {
			assert(errors.New("-ttl and -ttl-refresh must be specified together or not at all"))
		} else if *refreshTtl > 0 && *refreshTtl <= *refreshInterval {
			assert(errors.New("-ttl must be greater than -ttl-refresh"))
		}

		if *retryInterval <= 0 {
			assert(errors.New("-retry-interval must be greater than 0"))
		}

		dockerHost := os.Getenv("DOCKER_HOST")
		if dockerHost == "" {
			os.Setenv("DOCKER_HOST", "unix:///tmp/docker.sock")
		}

		docker, err := dockerapi.NewClientFromEnv()
		assert(err)

		if *deregister != "always" && *deregister != "on-success" {
			assert(errors.New("-deregister must be \"always\" or \"on-success\""))
		}

		b, err := bridge.New(docker, *registry, bridge.Config{
			HostIp:          *hostIp,
			Internal:        *internal,
			Global:          *global,
			ForceTags:       *forceTags,
			RefreshTtl:      *refreshTtl,
			RefreshInterval: *refreshInterval,
			DeregisterCheck: *deregister,
			Cleanup:         *cleanup,
		})

		assert(err)

		attempt := 0
		for *retryAttempts == -1 || attempt <= *retryAttempts {
			Log.Infof("Connecting to backend (%v/%v)", attempt, *retryAttempts)

			err = b.Ping()
			if err == nil {
				break
			}

			if err != nil && attempt == *retryAttempts {
				assert(err)
			}

			time.Sleep(time.Duration(*retryInterval) * time.Millisecond)
			attempt++
		}

		// Start event listener before listing containers to avoid missing anything
		events := make(chan *dockerapi.APIEvents)
		assert(docker.AddEventListener(events))
		Log.Infoln("Listening for Docker events ...")

		b.Sync(false)

		quit := make(chan struct{})

		// Start the TTL refresh timer
		if *refreshInterval > 0 {
			ticker := time.NewTicker(time.Duration(*refreshInterval) * time.Second)
			go func() {
				for {
					select {
					case <-ticker.C:
						b.Refresh()
					case <-quit:
						ticker.Stop()
						return
					}
				}
			}()
		}

		// Start the resync timer if enabled
		if *resyncInterval > 0 {
			resyncTicker := time.NewTicker(time.Duration(*resyncInterval) * time.Second)
			go func() {
				for {
					select {
					case <-resyncTicker.C:
						b.Sync(true)
					case <-quit:
						resyncTicker.Stop()
						return
					}
				}
			}()
		}

		// Process Docker events
		for msg := range events {
			switch msg.Status {
			case "start":
				go b.Add(msg.ID)
			case "die":
				go b.RemoveOnExit(msg.ID)
			}
		}

		close(quit)
		Log.Fatalln("Docker event loop closed") // todo: reconnect?

	}
	app.Run(os.Args)
}
