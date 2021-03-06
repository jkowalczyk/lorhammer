package main

import (
	"flag"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"lorhammer/src/lorhammer/command"
	"lorhammer/src/lorhammer/scenario"
	"lorhammer/src/model"
	"lorhammer/src/tools"
	"net/http"
	"runtime"
)

var VERSION string    // set at build time
var DATE_BUILD string // set at build time

var LOG = logrus.WithField("logger", "lorhammer/main")

func main() {
	version := flag.Bool("version", false, "Show current version and build time")
	localIp := flag.String("local-ip", "", "The address used by consul to access your metrics")
	consulAddr := flag.String("consul", "", "The ip:port of consul")
	nbGateway := flag.Int("nb-gateway", 0, "The number of gateway to launch")
	minNbNode := flag.Int("min-nb-node", 1, "The minimal number of node by gateway")
	maxNbNode := flag.Int("max-nb-node", 1, "The maximal number of node by gateway")
	nsAddress := flag.String("ns-address", "127.0.0.1:1700", "NetworkServer ip:port address")
	logInfo := flag.Bool("vv", false, "log infos")
	logDebug := flag.Bool("vvv", false, "log debugs")
	flag.Parse()

	if *version {
		logrus.WithFields(logrus.Fields{
			"version":    VERSION,
			"build time": DATE_BUILD,
			"go version": runtime.Version(),
		}).Warn("Welcome to the Lorhammer's Orchestrator")
		return
	}

	// LOGS
	if *logDebug {
		logrus.SetLevel(logrus.DebugLevel)
	} else if *logInfo {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
	}

	// PORT
	httpPort, err := tools.FreeTcpPort()
	if err != nil {
		LOG.WithError(err).Error("Free tcp port error")
	} else {
		LOG.WithField("port", httpPort).Info("Tcp port reserved")
	}

	// IP
	ip, err := tools.DetectIp(*localIp)
	if err != nil {
		LOG.WithError(err).Error("Ip error")
	} else {
		LOG.WithField("ip", ip).Info("Ip discovered")
	}

	// HOSTNAME
	hostname, err := tools.Hostname(ip, httpPort)
	if err != nil {
		LOG.WithError(err).Error("Hostname error")
	} else {
		LOG.WithField("hostname", hostname).Info("Unique hostname generated")
	}

	// PROMETHEUS
	prometheus := tools.NewPrometheus()

	// CONSUL/MQTT
	if *consulAddr == "" && *nbGateway <= 0 {
		LOG.Error("You need to specify at least -consul with ip:port")
		return
	}
	consulClient, err := tools.NewConsul(*consulAddr)
	if err != nil {
		LOG.WithError(err).Warn("Consul not found, lorhammer is in standalone mode")
	} else {
		if err := consulClient.Register(ip, hostname, httpPort); err != nil {
			LOG.WithError(err).Warn("Consul register error, lorhammer is in standalone mode")
		} else {
			mqttClient, err := tools.NewMqtt(hostname, consulClient)
			if err != nil {
				LOG.WithError(err).Warn("Mqtt not found, lorhammer is in standalone mode")
			} else {
				if err := mqttClient.Connect(); err != nil {
					LOG.WithError(err).Warn("Can't connect to mqtt, lorhammer is in standalone mode")
				}
				listenMqtt(mqttClient, []string{tools.MQTT_INIT_TOPIC, tools.MQTT_START_TOPIC + "/" + hostname}, hostname, prometheus)
			}
		}
	}

	// SCENARIO
	if *nbGateway > 0 {
		LOG.Warn("Launch manual scenario")
		sc, err := scenario.NewScenario(model.Init{
			NbGateway:         *nbGateway,
			NbNode:            [2]int{*minNbNode, *maxNbNode},
			NsAddress:         *nsAddress,
			ScenarioSleepTime: [2]string{"10s", "10s"},
			GatewaySleepTime:  [2]string{"100ms", "500ms"},
		})
		if err != nil {
			LOG.WithError(err).Fatal("Can't create scenario with infos passed in flags")
		}
		sc.Cron(prometheus)
	} else {
		LOG.Warn("No gateway, orchestrator will start scenarii")
	}

	// HTTP PART
	http.Handle("/metrics", promhttp.Handler())
	LOG.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil))
}

func listenMqtt(mqttClient tools.Mqtt, topics []string, hostname string, prometheus tools.Prometheus) {
	if err := mqttClient.HandleCmd(topics, func(cmd model.CMD) {
		command.ApplyCmd(cmd, mqttClient, hostname, prometheus)
	}); err != nil {
		LOG.WithError(err).WithField("topics", topics).Error("Error while subscribing")
	} else {
		logrus.WithField("topics", topics).Info("Listen mqtt")
	}
}
