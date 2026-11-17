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
var triggerPublisher *transport.BufferedPublisher
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
	var publisher transport.Publisher = transport.NopPublisher{}
	if *Config.Broker != "" {
		client = MQTT.NewClient(Config.GetMQTTOpts(handleOnConnection, handleOnConnectionLost))
		publisher = transport.NewMQTTPublisher(client)
		triggerPublisher = transport.NewBufferedPublisher(publisher, client.IsConnectionOpen, 256)
		trigger.Publisher = triggerPublisher
		trigger.MqttClient = client
		sensorWorker.MqttClient = client
		statusWorker.Availability = Config.TopicConfigurations.Availability
	} else {
		trigger.Publisher = publisher
	}
	statusWorker.Publisher = publisher
	sensorWorker.Publisher = publisher

	//register trigger and sensors
	workerLifecycle.Lock()
	applyWorkerConfig()
	workerLifecycle.Unlock()

	//webhook server: synchronous HTTP ingress next to MQTT
	webServer = webhook.NewServer(*Config.WebAddress, &trigger, &sensorWorker, startTime)
	if client != nil {
		webServer.MQTTConnected = client.IsConnectionOpen
	}
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
	if client != nil {
		// Paho retries the initial connection in the background. Local workers
		// and HTTP are already ready to serve requests.
		slog.Info("Connecting to MQTT broker in background")
		client.Connect()
	}

	//wait for shutdown signal or config reload
	for {
		select {
		case <-signals:
			shutdown(client)
			return
		case <-reloadSignals:
			handleReload(client)
		}
	}
}

// handleReload applies a configuration reload on SIGHUP. On any validation
// error the reload is aborted and the old configuration stays active.
func handleReload(client MQTT.Client) {
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
		slog.Warn("Failed to stop sensor worker cleanly", "error", err)
	}
	if err := trigger.Close(20 * time.Second); err != nil {
		slog.Warn("Failed to stop trigger worker cleanly", "error", err)
	}

	//swap the active configuration and restart the workers
	Config.TopicConfigurations = configuration
	applyWorkerConfig()

	if client != nil && client.IsConnectionOpen() {
		publishDiscovery()
	}
	slog.Info("Configuration reloaded.")
}

func publishDiscovery() {
	if !*Config.HomeassistantEnable {
		return
	}
	haClient := hassio.Client{
		DeviceName:  *Config.DeviceName,
		DeviceId:    *Config.DeviceId,
		TopicPrefix: *Config.HomeassistantTopic,
		Publisher:   statusWorker.Publisher,
	}
	if err := haClient.PublishDiscoveryConfig(Config.TopicConfigurations); err != nil {
		slog.Error("Error while publishing homeassistant discovery config", "error", err)
	}
}

var handleOnConnection = func(client MQTT.Client) {
	workerLifecycle.Lock()
	defer workerLifecycle.Unlock()
	if workersStopped || !client.IsConnectionOpen() {
		return
	}

	slog.Info("MQTT connected")
	triggerPublisher.Reset()
	trigger.ReInitialise()
	sensorWorker.ReInitialise(byte(*Config.SubscribeQOS))
	statusWorker.Online()
	publishDiscovery()
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
	closeables := []closable{&sensorWorker, &trigger, webServer, commandExecutor}

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
		if err := triggerPublisher.Close(2 * time.Second); err != nil {
			slog.Warn("MQTT trigger updates could not be flushed", "error", err)
		}
		if client.IsConnectionOpen() {
			statusWorker.Offline()
		}
		client.Disconnect(1000)
	}
}
