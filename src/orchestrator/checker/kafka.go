package checker

import (
	"encoding/json"
	"github.com/Shopify/sarama"
	"github.com/Sirupsen/logrus"
	"lorhammer/src/tools"
	"regexp"
)

const KafkaType = Type("kafka")

var _LOG_KAFKA = logrus.WithField("logger", "orchestrator/checker/kafka")

type kafka struct {
	config      kafkaConfig
	newConsumer func(addrs []string, config *sarama.Config) (sarama.Consumer, error)
	success     []CheckerSuccess
	err         []CheckerError
	poison      chan bool
}

type kafkaSuccess struct {
	check kafkaCheck
}

func (k kafkaSuccess) Details() map[string]interface{} {
	details := make(map[string]interface{})
	details["success"] = k.check.Description
	return details
}

type kafkaError struct {
	reason string
	value  string
}

func (k kafkaError) Details() map[string]interface{} {
	details := make(map[string]interface{})
	details["reason"] = k.reason
	details["value"] = k.value
	return details
}

type kafkaConfig struct {
	Address []string     `json:"address"`
	Topic   string       `json:"topic"`
	Checks  []kafkaCheck `json:"checks"`
}

type kafkaCheck struct {
	Description string   `json:"description"`
	Remove      []string `json:"remove"`
	Text        string   `json:"text"`
}

func newKafka(_ tools.Consul, rawConfig json.RawMessage) (Checker, error) {
	var kafkaConfig = kafkaConfig{}
	if err := json.Unmarshal(rawConfig, &kafkaConfig); err != nil {
		return nil, err
	}

	poison := make(chan bool)
	k := &kafka{config: kafkaConfig, poison: poison, newConsumer: sarama.NewConsumer}

	return k, nil
}

func (k *kafka) Start() error {
	kafkaConsumer, err := k.newConsumer(k.config.Address, nil)
	if err != nil {
		_LOG_KAFKA.WithError(err).Error("Kafka new consumer")
		return err
	}

	partitionList, err := kafkaConsumer.Partitions(k.config.Topic)
	if err != nil {
		_LOG_KAFKA.WithError(err).Error("Kafka partitions")
		return err
	}

	for partition := range partitionList {
		pc, err := kafkaConsumer.ConsumePartition(k.config.Topic, int32(partition), sarama.OffsetNewest)
		if err != nil {
			_LOG_KAFKA.WithError(err).Error("Kafka consume partition")
			return err
		}
		go k.handleMessage(kafkaConsumer, pc)
	}
	return nil
}

func (k *kafka) handleMessage(kafkaConsumer sarama.Consumer, pc sarama.PartitionConsumer) {
	quit := false
	for {
		select {
		case message := <-pc.Messages():
			atLeastMatch := false
			for _, check := range k.config.Checks {

				/**Here we strip the value to check from all the dynamically produced values (applicationID, devEUI...)
				These values are specified in the remove field through the json scenario in the kafka check section **/
				var s = string(message.Value)
				for _, dynamicValueToRemove := range check.Remove {
					var re = regexp.MustCompile(dynamicValueToRemove)
					s = re.ReplaceAllLiteralString(s, ``)
				}
				_LOG_KAFKA.Warn(s)
				if s == check.Text {
					atLeastMatch = true
					k.success = append(k.success, kafkaSuccess{check: check})
					_LOG_KAFKA.WithField("description", check.Description).Info("Success")
					break
				}
			}
			if !atLeastMatch {
				_LOG_KAFKA.Error("Result mismatch")
				k.err = append(k.err, kafkaError{reason: "Result mismatch", value: string(message.Value)})
			}
		case <-k.poison:
			quit = true
		}
		if quit {
			break
		}
	}
	pc.Close()
	kafkaConsumer.Close()
}

func (k *kafka) Check() ([]CheckerSuccess, []CheckerError) {
	k.poison <- true
	close(k.poison)
	if len(k.err) == 0 && len(k.success) == 0 {
		_LOG_KAFKA.Error("No message received from kafka")
		k.err = append(k.err, kafkaError{reason: "No message received from kafka", value: ""})
	}
	return k.success, k.err
}
