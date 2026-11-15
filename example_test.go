package smstunnel_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/narcisbodea/smstunnel-go"
)

func ExampleClient_SendSMS() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.SendSMS(context.Background(), "+14155550123", "Your order #1042 has shipped.",
		smstunnel.WithIdempotencyKey("order-1042-shipped"))
	if err != nil {
		log.Fatal(err)
	}
	if res.Queued {
		fmt.Println("Your phone is offline: the text goes out when it is back.")
	}
	fmt.Println("message", res.MessageID)
}

func ExampleClient_SendSignInCode() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// The text must contain the code as a number of its own, 4 to 10 digits.
	if _, err := client.SendSignInCode(context.Background(), "+14155550123", "Your sign-in code is 482913"); err != nil {
		log.Fatal(err)
	}
}

func ExampleWithSendAt() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	tomorrow := time.Now().AddDate(0, 0, 1)
	at := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, time.Local) // tomorrow at 09:00, this computer's time
	res, err := client.SendSMS(context.Background(), "+14155550123", "Reminder: your appointment is today at 10:30.",
		smstunnel.WithSendAt(at), smstunnel.WithIdempotencyKey("appointment-981-reminder"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("scheduled", res.Scheduled.ID, "for", res.Scheduled.SendAt.Local().Format(time.RFC1123))
}

func ExampleClient_ListScheduledTexts() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	page, err := client.ListScheduledTexts(context.Background(), smstunnel.ScheduledTextsQuery{}) // the ones still waiting, soonest first
	if err != nil {
		log.Fatal(err)
	}
	for _, text := range page.Texts {
		fmt.Println(text.ID, text.SendAt.Local().Format(time.RFC1123), text.To)
	}
	fmt.Printf("%d of the %d texts your plan lets wait at once\n", page.Waiting, page.Max)
}

func ExampleClient_CancelScheduledText() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	text, err := client.CancelScheduledText(context.Background(), "6738c0ffee0123456789abcd")
	var apiErr *smstunnel.APIError
	switch {
	case err == nil:
		fmt.Println(text.Status) // canceled
	case errors.As(err, &apiErr) && apiErr.Code == smstunnel.CodeScheduleNotEditable:
		fmt.Println("too late: it already went out, failed or was cancelled")
	default:
		log.Fatal(err)
	}
}

func ExampleClient_GetMessageStatus() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	status, err := client.GetMessageStatus(context.Background(), "6aea38f9b38ed3bed4f8569f")
	if err != nil {
		log.Fatal(err)
	}
	switch status.State {
	case smstunnel.StateSent, smstunnel.StateDelivered:
		fmt.Println("sent to", status.PhoneNumber)
	case smstunnel.StateFailed:
		fmt.Println("failed:", status.ErrorMessage)
	case smstunnel.StateUnconfirmed:
		fmt.Println("most likely sent; don't resend it")
	default:
		fmt.Println("waiting for the phone")
	}
}

func ExampleClient_ListPhones() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	phones, err := client.ListPhones(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, phone := range phones {
		fmt.Println(phone.ID, phone.Name, phone.Online)
	}
}

func ExampleClient_GetUsage() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	usage, err := client.GetUsage(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s plan: %d texts today, %s left\n", usage.Plan, usage.Used.SMSClean, usage.Remaining.SMSClean)
}

func ExampleClient_ListReceivedTexts() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	page, err := client.ListReceivedTexts(context.Background(), smstunnel.ReceivedTextsQuery{
		Since: time.Now().Add(-24 * time.Hour),
		Limit: 20,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, text := range page.Texts {
		fmt.Println(text.ReceivedAt.Format(time.Kitchen), text.Sender, text.Message)
	}
}

func ExampleReceivedTextsCursor() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	cursor, err := client.NewReceivedTextsCursor(smstunnel.ReceivedTextsCursorOptions{Since: time.Now()})
	if err != nil {
		log.Fatal(err)
	}
	for {
		texts, err := cursor.Next(context.Background())
		if err != nil {
			log.Print(err) // nothing is lost: the next call asks again
		}
		for _, text := range texts {
			fmt.Println(text.Sender, text.Message)
		}
		time.Sleep(10 * time.Second)
	}
}

func ExampleAPIError() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	_, err = client.SendSMS(context.Background(), "+14155550123", "Your table is ready.")

	var apiErr *smstunnel.APIError
	var connErr *smstunnel.ConnectionError
	switch {
	case err == nil:
		fmt.Println("sent")
	case errors.Is(err, smstunnel.ErrRateLimited) && errors.As(err, &apiErr) && apiErr.RetryAfter > 0:
		fmt.Println("too many texts right now; try again in", apiErr.RetryAfter)
	case errors.Is(err, smstunnel.ErrUnauthorized):
		fmt.Println("check the API key in the dashboard: Apps")
	case errors.As(err, &apiErr) && apiErr.Code == smstunnel.CodePermissionRequired:
		fmt.Println("the key lacks", apiErr.Permission)
	case errors.As(err, &connErr):
		fmt.Println("no answer; retry only with the same idempotency key:", connErr.Timeout())
	default:
		fmt.Println(err)
	}
}

func ExampleWithHTTPClient() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"),
		smstunnel.WithHTTPClient(&http.Client{Transport: http.DefaultTransport}),
		smstunnel.WithDefaultCountryCode("+40"),
		smstunnel.WithTimeout(15*time.Second),
		smstunnel.WithUserAgentSuffix("my-tool/2.1"),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = client
}
