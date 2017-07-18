package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	raven "github.com/getsentry/raven-go"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/mssola/user_agent"
	"github.com/streadway/amqp"
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
	appConfig := getConfig()
	raven.SetDSN(appConfig.SentryDSN)
	eventTypes := [3]string{"display", "click", "view"}

	conn, err := amqp.Dial(appConfig.RabbitMQAddr)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	for _, event := range eventTypes {
		ch.QueueDeclare(event,
			true,  // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
	}

	e := echo.New()
	// e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Pre(middleware.RemoveTrailingSlash())

	e.POST("/v2/event/:eventType", func(c echo.Context) (err error) {
		eventTypeParam := c.Param("eventType")
		eventAllowed := false
		for _, event := range eventTypes {
			if event == eventTypeParam {
				eventAllowed = true
			}
		}
		if eventAllowed == false {
			return echo.NewHTTPError(http.StatusBadRequest, "Type not found")
		}
		event := new(common.EventData)
		if err = c.Bind(event); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Bad params")
		}
		ua := user_agent.New(c.Request().UserAgent())
		name, version := ua.Browser()
		msg := &common.RabbitMSG{
			*event,
			common.BaseEvent{
				Date:     time.Now(),
				Mobile:   ua.Mobile(),
				Platform: ua.Platform(),
				OS:       ua.OS(),
				Browser:  name,
				Version:  version,
				IP:       c.RealIP(),
			},
		}
		body, _ := json.Marshal(msg)

		err = ch.Publish(
			"",             // exchange
			eventTypeParam, // routing key
			false,          // mandatory
			false,          // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        []byte(body),
			})
		if err != nil {
			raven.CaptureError(err, nil)
		}

		return c.String(http.StatusOK, "Ok")
	})

	failOnError(e.Start(":3030"), "Can't start server")
}
