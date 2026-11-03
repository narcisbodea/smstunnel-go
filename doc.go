// Package smstunnel sends texts (SMS) from your own Android phone, from your own number, with SMSTunnel
// (https://smstunnel.io).
//
// The phone runs the SMSTunnel app from Google Play and is linked to your SMSTunnel account. Your program hands each
// text to your account with an API key, and the phone sends it from its own SIM, so recipients see a number they know
// and can reply to. Create the key in your dashboard: Apps → Add an app (https://dashboard.smstunnel.io/apps).
//
//	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	res, err := client.SendSMS(ctx, "+14155550123", "Your order #1042 has shipped.")
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(res.MessageID)
//
// One call sends one text to one number: to text several people, call [Client.SendSMS] once per number. Sign-in and
// verification codes go through [Client.SendSignInCode]. [Client.GetMessageStatus], [Client.ListPhones],
// [Client.GetUsage] and [Client.ListReceivedTexts] read the rest; [ReceivedTextsCursor] follows received texts as
// they arrive.
//
// # Errors
//
// Every method returns an error when the call doesn't go through:
//
//   - [*APIError] when SMSTunnel answered with an error: it keeps the HTTP status, SMSTunnel's code (such as
//     [CodePermissionRequired]), its words and the Retry-After wait. errors.Is matches it against [ErrUnauthorized]
//     (401), [ErrForbidden] (403), [ErrNotFound] (404), [ErrRateLimited] (429) and [ErrServer] (5xx).
//   - [*ConnectionError] when no answer came back: SMSTunnel could not be reached, or didn't answer in time
//     ([ConnectionError.Timeout]).
//   - An error that matches [ErrInvalidInput] when an argument is refused before anything is sent.
//   - An error that matches context.Canceled when the context was cancelled.
//
// The client never retries by itself. After a timeout, a connection error or a 5xx answer a text may already be on
// its way: retry a send only with the same [WithIdempotencyKey], so it can't go out twice.
//
// A phone that is offline is not an error: the text waits on your account ([SendResult.Queued]) and goes out when
// the phone is back.
package smstunnel
