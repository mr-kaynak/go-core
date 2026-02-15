package service

import "fmt"

// System HTML templates for email rendering.
// These are the default full HTML templates used by system templates.
// They can be customized per-template via the html_content field in the database.

// systemEmailLayout is the shared base layout for all system email templates.
// Template-specific content is injected via the headerColor, headerContent, and bodyContent parameters.
func systemEmailLayout(headerColor, headerContent, bodyContent string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; margin: 0; padding: 0; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: %s; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background-color: #f9f9f9; }
        .button { display: inline-block; padding: 10px 20px; background-color: %s; color: white; text-decoration: none; border-radius: 5px; }
        .code { font-size: 32px; font-weight: bold; text-align: center; padding: 20px; background-color: #e9ecef; border-radius: 5px; letter-spacing: 5px; margin: 20px 0; }
        .footer { margin-top: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            %s
        </div>
        <div class="content">
            %s
        </div>
        <div class="footer">
            <p>&copy; {{.Year}} {{.AppName}}. All rights reserved.</p>
        </div>
    </div>
</body>
</html>`, headerColor, headerColor, headerContent, bodyContent)
}

var systemHTMLVerification = systemEmailLayout("#007bff",
	`<h1>{{.AppName}}</h1>`,
	`<h2>Hello {{.Username}},</h2>
            <p>Thank you for signing up! Please verify your email address by clicking the button below:</p>
            <p style="text-align: center;">
                <a href="{{.VerificationURL}}" class="button">Verify Email</a>
            </p>
            <p>Or copy and paste this link into your browser:</p>
            <p>{{.VerificationURL}}</p>
            <p>This link will expire in {{.ExpiresIn}}.</p>
            <p>If you didn't create an account, please ignore this email.</p>`,
)

var systemHTMLPasswordReset = systemEmailLayout("#dc3545",
	`<h1>Password Reset</h1>`,
	`<h2>Hello {{.Username}},</h2>
            <p>You requested to reset your password. Click the button below to create a new password:</p>
            <p style="text-align: center;">
                <a href="{{.ResetURL}}" class="button">Reset Password</a>
            </p>
            <p>Or copy and paste this link into your browser:</p>
            <p>{{.ResetURL}}</p>
            <p><strong>This link will expire in {{.ExpiresIn}}.</strong></p>
            <p>If you didn't request this, please ignore this email and your password will remain unchanged.</p>`,
)

var systemHTMLWelcome = systemEmailLayout("#28a745",
	`<h1>Welcome to {{.AppName}}!</h1>`,
	`<h2>Hello {{.Username}},</h2>
            <p>Welcome aboard! We're excited to have you as part of our community.</p>
            <p>Your account has been successfully created. You can now log in and start exploring all the features we have to offer.</p>
            <p style="text-align: center;">
                <a href="{{.LoginURL}}" class="button">Go to Login</a>
            </p>
            <p>If you have any questions, feel free to reach out to our support team.</p>
            <p>Best regards,<br>The {{.AppName}} Team</p>`,
)

var systemHTMLAccountLocked = systemEmailLayout("#ffc107",
	`<h1>Account Security Alert</h1>`,
	`<h2>Hello {{.Username}},</h2>
            <p>Your account has been temporarily locked due to {{.Reason}}.</p>
            <p>To unlock your account, please {{.Action}}.</p>
            <p>If you didn't attempt to access your account, please contact support immediately.</p>
            <p>Best regards,<br>{{.AppName}} Security Team</p>`,
)

var systemHTMLTwoFactor = systemEmailLayout("#6f42c1",
	`<h1>Security Code</h1>`,
	`<h2>Hello {{.Username}},</h2>
            <p>Your security code is:</p>
            <div class="code">{{.Code}}</div>
            <p>This code will expire in {{.ExpiresIn}}.</p>
            <p>If you didn't request this code, please secure your account immediately.</p>
            <p>Best regards,<br>{{.AppName}} Security Team</p>`,
)

var systemHTMLNotification = systemEmailLayout("#17a2b8",
	`<h1>{{.Subject}}</h1>`,
	`<p>{{.Message}}</p>`,
)
