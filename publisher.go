package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	raven "github.com/getsentry/raven-go"
	"github.com/gramework/gramework"
	"github.com/mssola/user_agent"
	"github.com/nats-io/go-nats"
	common "gitlab.com/maksimsharnin/attn-common"
)

type config struct {
	RabbitMQAddr string `json:"rabbitMQAddr"`
	SentryDSN    string `json:"sentryDSN"`
}

func failOnError(err error, msg string) {
	if err != nil {
		raven.CaptureError(err, nil)
		log.Fatalf("%s: %s", msg, err)
	}
}

func getConfig() config {
	file, _ := os.Open("config.json")
	decoder := json.NewDecoder(file)
	appConfig := config{}
	err := decoder.Decode(&appConfig)
	if err != nil {
		raven.CaptureError(err, nil)
		failOnError(err, "Can't read config")
	}
	return appConfig
}

func main() {
	nc, _ := nats.Connect(nats.DefaultURL)
	c, _ := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	defer c.Close()

	appConfig := getConfig()
	raven.SetDSN(appConfig.SentryDSN)
	eventTypes := [3]string{"display", "click", "view"}

	app := gramework.New()
	app.Sub("/v2").POST("event/:id", func(ctx *gramework.Context) {
		eventTypeParam, _ := ctx.RouteArgErr("id")
		eventAllowed := false
		for _, event := range eventTypes {
			if event == eventTypeParam {
				eventAllowed = true
			}
		}
		if eventAllowed == false {
			ctx.Error("Type not found", 400)
		}
		var event common.EventData
		ctx.UnJSON(&event)

		ua := user_agent.New(string(ctx.UserAgent()))
		name, version := ua.Browser()
		msg := &common.RabbitMSG{
			event,
			common.BaseEvent{
				Date:     time.Now(),
				Mobile:   ua.Mobile(),
				Platform: ua.Platform(),
				OS:       ua.OS(),
				Browser:  name,
				Version:  version,
				IP:       ctx.RemoteAddr().String(),
			},
		}
		c.Publish(eventTypeParam, msg)
	})

	app.ListenAndServe(":3030")
}
