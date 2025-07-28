package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/streadway/amqp"
)

// Config holds the configuration for the IoT device controller
type Config struct {
AMIURL         string `json:"ami_url"`
AMIUsername    string `json:"ami_username"`
AMIPassword    string `json:"ami_password"`
DeviceAPIURL   string `json:"device_api_url"`
DeviceAPIKey   string `json:"device_api_key"`
DeviceAPIPassword string `json:"device_api_password"`
}

// IoTDevice represents an IoT device
type IoTDevice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DeviceType  string `json:"device_type"`
	IPAddress   string `json:"ip_address"`
	Port        int    `json:"port"`
	Status      string `json:"status"`
}

var config Config
var devices []IoTDevice

func main() {
	loadConfig()
	setupAPI()
	setupRabbitMQ()
}

func loadConfig() {
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

 decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}
}

func setupAPI() {
	router := mux.NewRouter()
	router.HandleFunc("/devices", getDevices).Methods("GET")
	router.HandleFunc("/devices/{id}", getDevice).Methods("GET")
	router.HandleFunc("/devices/{id}/command", sendCommand).Methods("POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}

func setupRabbitMQ() {
	connection, err := amqp.Dial(config.AMIURL)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer channel.Close()

	err = channel.QueueDeclare(
		"",
		false,
		false,
		true,
		false,
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	msgs, err := channel.Consume(
		"",
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Printf("Received a message: %s", d.Body)
			handleMessage(d.Body)
		}
	}()

	log.Printf("Waiting for messages. To exit press CTRL+C, then enter.")
	<-forever
}

func handleMessage(body []byte) {
	var msg struct {
		DeviceID string `json:"device_id"`
		Command  string `json:"command"`
	}
	err := json.Unmarshal(body, &msg)
	if err != nil {
		log.Fatal(err)
	}

	// Send command to device API
	client := &http.Client{}
	req, err := http.NewRequest("POST", config.DeviceAPIURL+"/devices/"+msg.DeviceID+"/command", bytes.NewBufferString(`{"command":"`+msg.Command+`"}`))
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth(config.DeviceAPIKey, config.DeviceAPIPassword)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	log.Printf("Sent command to device %s: %s", msg.DeviceID, msg.Command)
}

func getDevices(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(devices)
}

func getDevice(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	for _, device := range devices {
		if device.ID == params["id"] {
			json.NewEncoder(w).Encode(device)
			return
		}
	}
}

func sendCommand(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	var command struct {
		Command string `json:"command"`
	}
	err := json.NewDecoder(r.Body).Decode(&command)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Send command to RabbitMQ
	connection, err := amqp.Dial(config.AMIURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer channel.Close()

	err = channel.Publish(
		"",
		"",
		false,
		false,
		amqp.Publishing{
			ContentType: "text/plain",
			Body: []byte(`{"device_id":"` + params["id"] + `","command":"` + command.Command + `"}`),
		},
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}