package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
	gha "github.com/sethvargo/go-githubactions"
)

type validatable interface {
	Validate() (bool, []string)
}

func main() {
	var err error

	command := gha.GetInput("COMMAND")
	gha.Debugf("Running command: %s", command)
	switch command {
	case "VALIDATE_STREAM_CONFIG":
		err = handleValidateStreamConfig()

	case "VALIDATE_CONSUMER_CONFIG":
		err = handleValidateConsumerConfig()

	case "DELETE_STREAM":
		err = handleDeleteStream()

	case "DELETE_CONSUMER":
		err = handleDeleteConsumer()

	case "CREATE_STREAM":
		err = handleCreateStream()

	case "CREATE_CONSUMER":
		err = handleCreateConsumer()

	case "UPDATE_STREAM":
		err = handleUpdateStream()

	case "PUBLISH":
		err = handlePublish()

	default:
		err = fmt.Errorf("invalid command '%s'", command)
	}

	if err != nil {
		gha.Fatalf("JetStream Action failed: %s", err)
	}
}

func handlePublish() error {
	subj := gha.GetInput("SUBJECT")
	if subj == "" {
		return fmt.Errorf("SUBJECT is required")
	}

	msg := gha.GetInput("MESSAGE")
	if msg == "" {
		return fmt.Errorf("MESSAGE is required")
	}

	shouldAck, err := strconv.ParseBool(gha.GetInput("SHOULD_ACK"))
	if err != nil {
		shouldAck = true
	}

	nc, err := connect()
	if err != nil {
		return err
	}

	log.Printf("Connected to %s", nc.ConnectedUrl())

	if shouldAck {
		resp, err := nc.Request(subj, []byte(msg), 5*time.Second)
		if err != nil {
			gha.SetOutput("response", err.Error())
			return fmt.Errorf("publish Request failed: %s", err)
		}

		if !jsm.IsOKResponse(resp) {
			gha.SetOutput("response", string(resp.Data))
			return fmt.Errorf("publish failed: %s", string(resp.Data))
		}

		log.Println(string(resp.Data))

		gha.SetOutput("response", string(resp.Data))

		return nil
	}

	err = nc.Publish(subj, []byte(msg))
	if err != nil {
		gha.SetOutput("response", err.Error())
		return err
	}

	err = nc.Flush()
	if err != nil {
		gha.SetOutput("response", err.Error())
		return err
	}

	gha.SetOutput("response", "published without requesting Ack")

	return nil
}

func handleUpdateStream() error {
	nc, err := connect()
	if err != nil {
		return err
	}
	log.Printf("Connected to %s", nc.ConnectedUrl())

	stream := gha.GetInput("STREAM")
	if stream == "" {
		return fmt.Errorf("STREAM is required")
	}

	cfile := gha.GetInput("CONFIG")
	if cfile == "" {
		return fmt.Errorf("CONFIG is required")
	}

	cj, err := ioutil.ReadFile(cfile)
	if err != nil {
		return err
	}

	var cfg api.StreamConfig
	err = json.Unmarshal(cj, &cfg)
	if err != nil {
		return err
	}

	str, err := jsm.LoadStream(stream, jsm.WithConnection(nc))
	if err != nil {
		return err
	}

	err = str.UpdateConfiguration(cfg)
	if err != nil {
		return err
	}

	str.Reset()

	cj, err = json.Marshal(str.Configuration())
	if err != nil {
		return err
	}
	gha.SetOutput("config", string(cj))

	log.Printf("Configuration: %s", string(cj))

	return nil
}

func handleDeleteConsumer() error {
	nc, err := connect()
	if err != nil {
		return err
	}
	log.Printf("Connected to %s", nc.ConnectedUrl())

	stream := gha.GetInput("STREAM")
	if stream == "" {
		return fmt.Errorf("STREAM is required")
	}

	consumer := gha.GetInput("CONSUMER")
	if consumer == "" {
		return fmt.Errorf("CONSUMER is required")
	}

	missingok, err := strconv.ParseBool(gha.GetInput("MISSING_OK"))
	if err != nil {
		missingok = false
	}

	known, err := jsm.IsKnownStream(stream, jsm.WithConnection(nc))
	if err != nil {
		return err
	}

	if missingok && !known {
		log.Printf("Stream %s was not present", stream)
		return nil
	}

	if !known {
		gha.Fatalf("Stream %s does not exist", stream)
	}

	known, err = jsm.IsKnownConsumer(stream, consumer, jsm.WithConnection(nc))
	if err != nil {
		return err
	}

	if missingok && !known {
		log.Printf("Consumer %s > %s was not present", stream, consumer)
		return nil
	}

	if !known {
		gha.Fatalf("Consumer %s > %s does not exist", stream, consumer)
	}

	cons, err := jsm.LoadConsumer(stream, consumer, jsm.WithConnection(nc))
	if err != nil {
		return err
	}

	return cons.Delete()
}

func handleDeleteStream() error {
	nc, err := connect()
	if err != nil {
		return err
	}
	log.Printf("Connected to %s", nc.ConnectedUrl())

	stream := gha.GetInput("STREAM")
	if stream == "" {
		return fmt.Errorf("STREAM is required")
	}

	missingok, err := strconv.ParseBool(gha.GetInput("MISSING_OK"))
	if err != nil {
		missingok = false
	}

	known, err := jsm.IsKnownStream(stream, jsm.WithConnection(nc))
	if err != nil {
		return err
	}

	if missingok && !known {
		log.Printf("Stream %s was not present", stream)
		return nil
	}

	if !known {
		gha.Fatalf("Stream %s does not exist, cannot delete it", stream)
	}

	str, err := jsm.LoadStream(stream, jsm.WithConnection(nc))
	if err != nil {
		return err
	}

	return str.Delete()
}

func handleCreateStream() error {
	nc, err := connect()
	if err != nil {
		return err
	}
	log.Printf("Connected to %s", nc.ConnectedUrl())

	cfile := gha.GetInput("CONFIG")
	if cfile == "" {
		return fmt.Errorf("CONFIG is required")
	}

	cj, err := ioutil.ReadFile(cfile)
	if err != nil {
		return err
	}

	var cfg api.StreamConfig
	err = json.Unmarshal(cj, &cfg)
	if err != nil {
		return err
	}

	stream, err := jsm.NewStreamFromDefault(cfg.Name, cfg, jsm.StreamConnection(jsm.WithConnection(nc)))
	if err != nil {
		return err
	}

	cj, err = json.Marshal(stream.Configuration())
	if err != nil {
		return err
	}
	gha.SetOutput("config", string(cj))

	log.Printf("Configuration: %s", string(cj))

	return nil
}

func handleCreateConsumer() error {
	nc, err := connect()
	if err != nil {
		return err
	}
	log.Printf("Connected to %s", nc.ConnectedUrl())

	cfile := gha.GetInput("CONFIG")
	if cfile == "" {
		return fmt.Errorf("CONFIG is required")
	}

	stream := gha.GetInput("STREAM")
	if stream == "" {
		return fmt.Errorf("STREAM is required")
	}

	cj, err := ioutil.ReadFile(cfile)
	if err != nil {
		return err
	}

	var cfg api.ConsumerConfig
	err = json.Unmarshal(cj, &cfg)
	if err != nil {
		return err
	}

	consumer, err := jsm.NewConsumerFromDefault(stream, cfg, jsm.ConsumerConnection(jsm.WithConnection(nc)))
	if err != nil {
		return err
	}

	cj, err = json.Marshal(consumer.Configuration())
	if err != nil {
		return err
	}
	gha.SetOutput("config", string(cj))

	log.Printf("Configuration: %s", string(cj))

	return nil
}

func connect() (*nats.Conn, error) {
	creds := gha.GetInput("CREDENTIALS")
	user := gha.GetInput("USERNAME")
	pass := gha.GetInput("PASSWORD")

	server := gha.GetInput("SERVER")
	if server == "" {
		return nil, fmt.Errorf("SERVER is required")
	}

	opts := []nats.Option{
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			gha.Fatalf("NATS Error: %s", err)
		}),
	}

	if user != "" {
		opts = append(opts, nats.UserInfo(user, pass))
	}

	if creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}

	return nats.Connect(server, opts...)
}

func handleValidateStreamConfig() error {
	cfg := &api.StreamConfig{}
	cfile, valid, errs, err := validateHelper("CONFIG", cfg)
	if err != nil {
		return err
	}

	if valid {
		log.Printf("%s is a valid Stream Configuration", cfile)
		return nil
	}

	return fmt.Errorf("%s is an invalid JetStream Stream configuration:\n\t%s\n", cfile, strings.Join(errs, "\n\t"))
}

func handleValidateConsumerConfig() error {
	cfg := &api.ConsumerConfig{}
	cfile, valid, errs, err := validateHelper("CONFIG", cfg)
	if err != nil {
		return err
	}

	if valid {
		log.Printf("%s is a valid Consumer Configuration", cfile)
		return nil
	}

	return fmt.Errorf("%s is an invalid JetStream Consumer configuration:\n\t%s\n", cfile, strings.Join(errs, "\n\t"))
}

func validateHelper(input string, cfg validatable) (string, bool, []string, error) {
	cfile := gha.GetInput(input)
	if cfile == "" {
		return "", false, nil, fmt.Errorf("%s is required", input)
	}

	cb, err := ioutil.ReadFile(cfile)
	if err != nil {
		return cfile, false, nil, err
	}

	err = json.Unmarshal(cb, cfg)
	if err != nil {
		return cfile, false, nil, fmt.Errorf("could not parse '%s': %s", cfile, err)
	}

	ok, errs := cfg.Validate()

	return cfile, ok, errs, nil
}
