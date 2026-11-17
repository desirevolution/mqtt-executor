package main

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/rainu/mqtt-executor/internal/cmd"
	"github.com/rainu/mqtt-executor/internal/mqtt"
	"github.com/rainu/mqtt-executor/internal/mqtt/hassio"
	"github.com/rainu/mqtt-executor/internal/transport"
	"github.com/rainu/mqtt-executor/internal/webhook"
)

var commandExecutor *cmd.CommandExecutor
var statusWorker mqtt.StatusWorker
var sensorWorker mqtt.SensorWorker
var trigger mqtt.Trigger
var webServer *webhook.Server

var workerLifecycle sync.Mutex
var workersStopped bool

// applyWorkerConfig (re-)starts the trigger and sensor workers with the
// active configuration (Config.TopicConfigurations). It is used at startup
// and on hot reload; the workers must be closed before it runs again.
func applyWorkerConfig() {
	trigger.Initialise(byte(*Config.SubscribeQOS), byte(*Config.PublishQOS), Config.TopicConfigurations.Trigger)
	sensorWorker.Initialise(byte(*Config.PublishQOS), Config.TopicConfigurations.Sensors())
	sensorWorker.ReInitialise(byte(*Config.SubscribeQOS))
}

func main() {
	startTime := time.Now()
	LoadConfig()
	commandExecutor = cmd.NewCommandExecutor()
	trigger.Executor = commandExecutor
	sensorWorker.Executor = commandExecutor

	//reacting to signals (interrupt) and config reload (SIGHUP)
	signals := make(chan os.Signal, 1)
	reloadSignals := make(chan os.Signal, 1)
	defer signal.Stop(signals)
	defer signal.Stop(reloadSignals)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	signal.Notify(reloadSignals, syscall.SIGHUP)

	var client MQTT.Client
	var publisher transport.Publisher
	if *Config.Broker != "" {
		client = MQTT.NewClient(Config.GetMQTTOpts(
			handleOnConnection,
			handleOnConnectionLost,
		))
		publisher = transport.NewMQTTPublisher(client)
		trigger.MqttClient = client
		sensorWorker.MqttClient = client

		if token := client.Connect(); token.Wait() && token.Error() != nil {
			fatal("Error while connecting to mqtt broker", "error", token.Error())
		}
	} else {
		//webhook-only mode: trigger/sensor results are only available through
		//the ingress (see ticket 06), publishes are discarded
		publisher = transport.NopPublisher{}
	}
	statusWorker.Publisher = publisher
	trigger.Publisher = publisher
	sensorWorker.Publisher = publisher

	//if hassio is enabled -> publish the hassio mqtt-discovery configs
	//(broker mode only, see LoadConfig validation)
	if *Config.HomeassistantEnable && client != nil {
		haClient := hassio.Client{
			DeviceName:  *Config.DeviceName,
			DeviceId:    *Config.DeviceId,
			TopicPrefix: *Config.HomeassistantTopic,
			Publisher:   publisher,
		}
		if err := haClient.PublishDiscoveryConfig(Config.TopicConfigurations); err != nil {
			slog.Error("Error while publishing homeassistant discovery config", "error", err)
		}
	}

	if Config.TopicConfigurations.Availability != nil && client != nil {
		statusWorker.Initialise(*Config.TopicConfigurations.Availability)
	}

	//register trigger and sensors
	workerLifecycle.Lock()
	applyWorkerConfig()
	workerLifecycle.Unlock()

	//webhook server: synchronous HTTP ingress next to MQTT
	webServer = webhook.NewServer(*Config.WebAddress, &trigger, &sensorWorker, startTime)
	if *Config.WebAddress != "" {
		if host, _, err := net.SplitHostPort(*Config.WebAddress); err == nil {
			switch host {
			case "", "0.0.0.0", "::":
				slog.Warn("Webhook server binds to all interfaces without authentication!")
			}
		}
		go func() {
			slog.Info("Webhook server listening", "address", *Config.WebAddress)
			if err := webServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				//a broken webhook server makes the process pointless - exit
				fatal("Webhook server failed", "error", err)
			}
		}()
	}

	//wait for shutdown signal or config reload
	for {
		select {
		case <-signals:
			shutdown(client)
			return
		case <-reloadSignals:
			handleReload(client, publisher)
		}
	}
}

// handleReload applies a configuration reload on SIGHUP. On any validation
// error the reload is aborted and the old configuration stays active.
func handleReload(client MQTT.Client, publisher transport.Publisher) {
	slog.Info("SIGHUP received. Reloading configuration...")

	configuration, err := loadReloadConfig()
	if err != nil {
		//abort the reload: the old configuration stays active
		slog.Error("Configuration reload aborted, keeping the old configuration", "error", err)
		return
	}

	workerLifecycle.Lock()
	defer workerLifecycle.Unlock()

	//stop the trigger and sensor workers (running interval commands are
	//cancelled; running trigger commands may finish). Note: if a Close hits
	//its timeout, the worker's waiter goroutine lingers until the next Close
	//(commands are process-group killed, so this is a rare edge)
	if err := sensorWorker.Close(20 * time.Second); err != nil {
		slog.Warn("Timeout while stopping sensor worker", "error", err)
	}
	if err := trigger.Close(20 * time.Second); err != nil {
		slog.Warn("Timeout while stopping trigger worker", "error", err)
	}

	//swap the active configuration and restart the workers
	Config.TopicConfigurations = configuration
	applyWorkerConfig()

	//refresh the homeassistant discovery for new/changed entities (broker mode)
	if *Config.HomeassistantEnable && client != nil {
		haClient := hassio.Client{
			DeviceName:  *Config.DeviceName,
			DeviceId:    *Config.DeviceId,
			TopicPrefix: *Config.HomeassistantTopic,
			Publisher:   publisher,
		}
		if err := haClient.PublishDiscoveryConfig(Config.TopicConfigurations); err != nil {
			slog.Error("Error while publishing homeassistant discovery config", "error", err)
		}
	}

	slog.Info("Configuration reloaded.")
}

var handleOnConnection = func(client MQTT.Client) {
	workerLifecycle.Lock()
	defer workerLifecycle.Unlock()
	if workersStopped || !trigger.IsInitialised() {
		return
	}

	slog.Info("Reinitialise...")
	trigger.ReInitialise()
	sensorWorker.ReInitialise(byte(*Config.SubscribeQOS))
}

var handleOnConnectionLost = func(client MQTT.Client, err error) {
	slog.Warn("Connection lost to broker.", "error", err)
}

func shutdown(client MQTT.Client) {
	slog.Info("Shutting down...")
	workerLifecycle.Lock()
	workersStopped = true
	workerLifecycle.Unlock()

	type closable interface {
		Close(time.Duration) error
	}
	closeables := []closable{&statusWorker, &sensorWorker, &trigger, webServer, commandExecutor}

	//most operating systems wait a maximum of 30 seconds

	wg := sync.WaitGroup{}
	wg.Add(len(closeables))
	timeout := 20 * time.Second

	for _, c := range closeables {
		go func(c closable) {
			defer wg.Done()

			if err := c.Close(timeout); err != nil {
				slog.Error("Timeout while waiting for graceful shutdown!", "error", err)
			}
		}(c)
	}
	wg.Wait()

	//we have to disconnect at last because one closeable unsubscripe all topics
	if client != nil {
		client.Disconnect(10 * 1000) //wait 10sek at most
	}
}
