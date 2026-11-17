package mqtt

// MQTT topic suffixes and well-known payload strings shared by the trigger
// state machine and the sensor worker.
const (
	//topic suffixes
	TopicSuffixState  = "STATE"
	TopicSuffixResult = "RESULT"

	//trigger control payloads
	PayloadStart = "START"
	PayloadStop  = "STOP"

	//trigger state payloads
	PayloadStatusRunning = "RUNNING"
	PayloadStatusStopped = "STOPPED"

	//result payloads
	PayloadResultInterrupted  = "<INTERRUPTED>"
	PayloadResultFailedPrefix = "<FAILED>;"
)
