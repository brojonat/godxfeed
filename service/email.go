package service

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

func EmailToken(s Service, email, token string) error {
	client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))

	from := mail.NewEmail("DX Feed", os.Getenv("SENDGRID_SENDER_EMAIL")) // adjust sender details
	to := mail.NewEmail("", email)
	subject := "Your DX Feed Access Token"

	// Create plain text content
	plainContent := fmt.Sprintf("Thank you for your support! Your access token is: %s", token)

	// Create HTML content
	htmlContent := fmt.Sprintf(`
		<html>
			<body>
				<h2>Thank you for your support!</h2>
				<p>Your access token is:</p>
				<pre style="background-color: #f4f4f4; padding: 15px; border-radius: 5px;">%s</pre>
				<p>You can use this token to access the DX Feed API.</p>
				<p>If you have any questions, please don't hesitate to reach out.</p>
			</body>
		</html>
	`, token)

	message := mail.NewSingleEmail(from, subject, to, plainContent, htmlContent)

	response, err := client.Send(message)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	// Log the response status
	s.Log(int(slog.LevelDebug), "SendGrid email sent",
		"statusCode", response.StatusCode,
		"recipient", to.Address)

	return nil
}
