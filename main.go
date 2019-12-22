package main

import (
	"flag"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const (
	noDequeue           = "The original message is left intact."
	deQueueError        = "ERROR dequeueing message ID %v : %v"
	errorSendingMessage = "\nERROR sending message to destination %v\n\n"
	actionMove          = "Moving"
	actionCopy          = "Copying"
)

type QueueOperationsRequest struct {
	SourceQueue string
	DestQueue   string
	MessageID   string
	List        bool
	NoDelete    bool
}

type SQSClient struct {
	AWSSQSClient sqs.SQS
	MessageCount int
}

func NewSQSClient() (*SQSClient, error) {

	// enable automatic use of AWS_PROFILE like aws cli and other tools do.
	opts := session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}

	session, err := session.NewSessionWithOptions(opts)
	if err != nil {
		panic(err)
	}

	return &SQSClient{
		AWSSQSClient: *sqs.New(session),
		MessageCount: 0,
	}, nil
}

func (c *SQSClient) ListMessages(request QueueOperationsRequest) {

	log.Printf("List of Messages in Queue:\t%s\n", request.SourceQueue)

	maxMessages := int64(10)
	waitTime := int64(0)
	messageAttributeNames := aws.StringSlice([]string{"All"})

	rmin := &sqs.ReceiveMessageInput{
		QueueUrl:              &request.SourceQueue,
		MaxNumberOfMessages:   &maxMessages,
		WaitTimeSeconds:       &waitTime,
		MessageAttributeNames: messageAttributeNames,
	}

	lastMessageCount := int(1)
	// loop as long as there are messages on the queue
	for {
		resp, err := c.AWSSQSClient.ReceiveMessage(rmin)

		if err != nil {
			panic(err)
		}
		c.MessageCount = c.MessageCount + len(resp.Messages)

		if lastMessageCount == 0 && len(resp.Messages) == 0 {
			// no messages returned twice now, the queue is probably empty
			//log.Printf("done")
			log.Printf("Message Count: %d\n\n", c.MessageCount)
			return
		}

		lastMessageCount = len(resp.Messages)

		for _, m := range resp.Messages {
			log.Printf("MessageId: %s  Body: %s\n", *m.MessageId, *m.Body)
		}
	}
}

func (c *SQSClient) MoveMessage(request QueueOperationsRequest) {
	log.Printf("Moving or Copy Message\n\tFrom Queue:\t%s\n\tTo Queue: \t%s\n\tMessageId: \t%s\n", request.SourceQueue, request.DestQueue, request.MessageID)

	maxMessages := int64(10)
	waitTime := int64(0)
	messageAttributeNames := aws.StringSlice([]string{"All"})

	rmin := &sqs.ReceiveMessageInput{
		QueueUrl:              &request.SourceQueue,
		MaxNumberOfMessages:   &maxMessages,
		WaitTimeSeconds:       &waitTime,
		MessageAttributeNames: messageAttributeNames,
	}

	lastMessageCount := int(1)
	// loop as long as there are messages on the queue
	for {
		resp, err := c.AWSSQSClient.ReceiveMessage(rmin)
		if err != nil {
			panic(err)
		}

		if lastMessageCount == 0 && len(resp.Messages) == 0 {
			// no messages returned twice now, the queue is probably empty
			log.Printf("Messages Transferred: %d\n\n", c.MessageCount)
			return
		}

		lastMessageCount = len(resp.Messages)

		for _, m := range resp.Messages {
			if *m.MessageId == request.MessageID {
				// write the message to the destination queue
				smi := sqs.SendMessageInput{
					MessageAttributes: m.MessageAttributes,
					MessageBody:       m.Body,
					QueueUrl:          &request.DestQueue,
				}

				c.MessageCount = c.MessageCount + 1

				action := actionMove
				if request.NoDelete {
					action = actionCopy
				}

				log.Printf(">> %s MessageId: %s  Body: %s\n", action, *m.MessageId, *m.Body)

				_, err := c.AWSSQSClient.SendMessage(&smi)

				if err != nil {
					log.Printf(errorSendingMessage, err)
					return
				}

				dmi := &sqs.DeleteMessageInput{
					QueueUrl:      &request.SourceQueue,
					ReceiptHandle: m.ReceiptHandle,
				}

				if !request.NoDelete {
					if _, err := c.AWSSQSClient.DeleteMessage(dmi); err != nil {
						log.Printf(deQueueError,
							*m.ReceiptHandle,
							err)
					}
				}
				return
			}
		}
	}
}

func (c *SQSClient) MoveMessages(request QueueOperationsRequest) {

	log.Printf("Moving Messages\nFrom Queue:\t%s\nTo Queue: \t%s\n", request.SourceQueue, request.DestQueue)

	maxMessages := int64(10)
	waitTime := int64(0)
	messageAttributeNames := aws.StringSlice([]string{"All"})

	rmin := &sqs.ReceiveMessageInput{
		QueueUrl:              &request.SourceQueue,
		MaxNumberOfMessages:   &maxMessages,
		WaitTimeSeconds:       &waitTime,
		MessageAttributeNames: messageAttributeNames,
	}

	lastMessageCount := int(1)
	// loop as long as there are messages on the queue
	for {
		resp, err := c.AWSSQSClient.ReceiveMessage(rmin)
		if err != nil {
			panic(err)
		}

		// log.Printf(" >Messages Fetched: %d\n", len(resp.Messages))

		if lastMessageCount == 0 && len(resp.Messages) == 0 {
			// no messages returned twice now, the queue is probably empty
			log.Printf("Messages Transferred: %d\n\n", c.MessageCount)
			return
		}

		lastMessageCount = len(resp.Messages)
		// log.Printf("received %v messages...", len(resp.Messages))

		var wg sync.WaitGroup
		wg.Add(len(resp.Messages))

		for _, m := range resp.Messages {
			go func(m *sqs.Message) {
				defer wg.Done()

				// write the message to the destination queue
				smi := sqs.SendMessageInput{
					MessageAttributes: m.MessageAttributes,
					MessageBody:       m.Body,
					QueueUrl:          &request.DestQueue,
				}

				c.MessageCount = c.MessageCount + 1

				action := actionMove
				if request.NoDelete {
					action = actionCopy
				}
				log.Printf("%s MessageId: %s  Body: %s\n", action, *m.MessageId, *m.Body)

				_, err := c.AWSSQSClient.SendMessage(&smi)

				if err != nil {
					log.Printf(errorSendingMessage, err)
					return
				}

				// message was sent, dequeue from source queue
				dmi := &sqs.DeleteMessageInput{
					QueueUrl:      &request.SourceQueue,
					ReceiptHandle: m.ReceiptHandle,
				}

				if !request.NoDelete {
					if _, err := c.AWSSQSClient.DeleteMessage(dmi); err != nil {
						log.Printf(deQueueError,
							*m.ReceiptHandle,
							err)
					}
				}
			}(m)
		}

		// wait for all jobs from this batch...
		wg.Wait()
	}
}

func main() {

	client, _ := NewSQSClient()
	request := getCmdArguments()
	routeRequest(request, client)

}

func getCmdArguments() QueueOperationsRequest {

	sourceQueue := flag.String("src", "-BLANK-", "-src >queue>")
	destQueue := flag.String("dest", "-BLANK-", "-dest <queue>")
	messageId := flag.String("msgid", "-BLANK-", "-id <message id>")
	noDelete := flag.Bool("nodel", false, "-nodel")
	list := flag.Bool("l", false, "-l")
	flag.Parse()

	return QueueOperationsRequest{
		SourceQueue: *sourceQueue,
		DestQueue:   *destQueue,
		MessageID:   *messageId,
		List:        *list,
		NoDelete:    *noDelete,
	}
}

func routeRequest(req QueueOperationsRequest, client *SQSClient) bool {

	ran := false

	if req.List && len(req.SourceQueue) > 15 {
		// List it
		ran = true
		client.ListMessages(req)
	}

	if !ran && !req.List && len(req.SourceQueue) > 15 && len(req.DestQueue) > 15 && len(req.MessageID) > 10 {
		ran = true
		client.MoveMessage(req)
	}

	if !ran && !req.List && len(req.SourceQueue) > 15 && len(req.DestQueue) > 15 {
		ran = true
		client.MoveMessages(req)
	}

	return ran
}
