package email

import (
	"fmt"
	"os"

	"github.com/resend/resend-go/v2"
)

var client *resend.Client

func Init() {
	apiKey := os.Getenv("RESEND_API_KEY")
	client = resend.NewClient(apiKey)
}

func SendOTP(toEmail, studentName, otp string) error {
	if client == nil {
		return fmt.Errorf("resend client not initialized")
	}

	params := &resend.SendEmailRequest{
		From:    "Presenze <noreply@presenze.website>", // must be a domain verified in Resend
		To:      []string{toEmail},
		Subject: "Your Presenze password reset code",
		Html: fmt.Sprintf(`
			<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
				<h2 style="color:#111;">Password reset code</h2>
				<p>Hi %s,</p>
				<p>Use this code to reset your Presenze password. It expires in 10 minutes.</p>
				<div style="font-size: 32px; font-weight: 700; letter-spacing: 6px; background: #f4f4f5; padding: 16px 24px; border-radius: 8px; text-align: center; margin: 20px 0;">
					%s
				</div>
				<p style="color:#666; font-size: 13px;">If you didn't request this, you can safely ignore this email.</p>
			</div>
		`, studentName, otp),
	}

	_, err := client.Emails.Send(params)
	return err
}

func SendReportEmail(toEmail, professorName, subjectLabel, periodLabel string, attachment []byte, filename string) error {
	if client == nil {
		return fmt.Errorf("resend client not initialized")
	}

	params := &resend.SendEmailRequest{
		From:    "Presenze <noreply@presenze.website>",
		To:      []string{toEmail},
		Subject: fmt.Sprintf("Attendance report — %s (%s)", subjectLabel, periodLabel),
		Html: fmt.Sprintf(`
			<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
				<h2 style="color:#111;">Your attendance report is ready</h2>
				<p>Hi %s,</p>
				<p>Attached is your %s attendance report for <strong>%s</strong>, covering <strong>%s</strong>.</p>
				<p style="color:#666; font-size: 13px;">This is an automated report from your subscription settings in Presenze. You can manage or cancel this anytime from the Reports tab.</p>
			</div>
		`, professorName, periodLabel, subjectLabel, periodLabel),
		Attachments: []*resend.Attachment{
			{
				Filename: filename,
				Content:  attachment,
			},
		},
	}

	_, err := client.Emails.Send(params)
	return err
}
