package utils

import (
	"fmt"
	"html"
	"log"
	"os"
	"strconv"
	"strings"

	"gopkg.in/gomail.v2"
)

func SendEmail(to, subject, body string) error {
	host := os.Getenv("MAIL_HOST")
	portStr := os.Getenv("MAIL_PORT")
	username := os.Getenv("MAIL_USERNAME")
	password := os.Getenv("MAIL_PASSWORD")
	from := os.Getenv("MAIL_FROM_ADDRESS")

	port, _ := strconv.Atoi(portStr)

	m := gomail.NewMessage()
	m.SetHeader("From", from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	d := gomail.NewDialer(host, port, username, password)

	if err := d.DialAndSend(m); err != nil {
		log.Println("Error sending email:", err)
		return err
	}

	return nil
}

func SendOtpEmail(to string, name string, otp int) error {
	// Ambil variabel dari .env
	host := os.Getenv("MAIL_HOST")
	portStr := os.Getenv("MAIL_PORT")
	username := os.Getenv("MAIL_USERNAME")
	password := os.Getenv("MAIL_PASSWORD")
	from := os.Getenv("MAIL_FROM_ADDRESS")
	fromName := os.Getenv("MAIL_FROM_NAME")

	// Validasi variabel env
	if host == "" || portStr == "" || username == "" || password == "" || from == "" {
		return fmt.Errorf("email environment variables not set properly")
	}

	// Konversi port ke int
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 587
	}

	// Format body email
	subject := "Kode Verifikasi OTP - EduPilot"
	body := fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="id">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>OTP EduPilot</title>
	</head>
	<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #f3f4f6; padding: 40px 20px; margin: 0; line-height: 1.6;">
		<div style="max-width: 500px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; overflow: hidden; box-shadow: 0 4px 6px rgba(0,0,0,0.05);">
			
			<!-- Header -->
			<div style="background-color: #1e3a8a; padding: 24px; text-align: center;">
				<h1 style="margin: 0; font-size: 24px; color: #ffffff; font-weight: 700; letter-spacing: 1px;">EduPilot</h1>
			</div>

			<!-- Body -->
			<div style="padding: 32px;">
				<h2 style="color: #111827; font-size: 20px; margin-top: 0;">Halo, %s 👋</h2>
				<p style="color: #4b5563; font-size: 15px; margin-bottom: 24px;">
					Terima kasih telah bergabung dengan <strong>EduPilot</strong>. Untuk menyelesaikan pendaftaran, silakan masukkan kode verifikasi (OTP) berikut:
				</p>

				<div style="background-color: #eff6ff; border: 1px solid #bfdbfe; border-radius: 8px; padding: 20px; text-align: center; margin-bottom: 24px;">
					<span style="font-size: 32px; font-weight: 700; color: #1d4ed8; letter-spacing: 6px;">%06d</span>
				</div>

				<p style="color: #6b7280; font-size: 14px;">
					Kode OTP ini berlaku selama <strong>5 menit</strong>. Demi keamanan, jangan pernah membagikan kode ini kepada siapapun.
				</p>

				<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0;">

				<p style="color: #374151; font-size: 14px; margin: 0;">
					Salam hangat,<br>
					<strong>Tim EduPilot</strong>
				</p>
			</div>

			<!-- Footer -->
			<div style="background-color: #f9fafb; border-top: 1px solid #e5e7eb; padding: 16px; text-align: center;">
				<p style="margin: 0; color: #9ca3af; font-size: 12px;">
					&copy; 2026 EduPilot. Hak Cipta Dilindungi.
				</p>
			</div>

		</div>
	</body>
	</html>
	`, html.EscapeString(name), otp)

	// Setup email
	m := gomail.NewMessage()
	m.SetHeader("From", fmt.Sprintf("%s <%s>", fromName, from))
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	// Kirim email
	d := gomail.NewDialer(host, port, username, password)
	d.SSL = false // pakai STARTTLS (port 587)

	if err := d.DialAndSend(m); err != nil {
		log.Println("Failed to send OTP email:", err)
		return err
	}

	return nil
}

// SendResetPasswordEmail emails a reset link. student=true words it as a student
// account ("akun siswa"); the link itself already points at the right app.
func SendResetPasswordEmail(email string, link string, student bool) error {
	subject := "Reset Password Akun EduPilot"
	if student {
		subject = "Reset Password Akun Siswa EduPilot"
	}
	return SendEmail(email, subject, BuildResetPasswordEmail(link, student))
}

// BuildResetPasswordEmail renders the Indonesian reset-password email (link escaped).
func BuildResetPasswordEmail(link string, student bool) string {
	account := "akun"
	if student {
		account = "akun siswa"
	}
	return fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="id">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Reset Password EduPilot</title>
	</head>
	<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #f3f4f6; padding: 40px 20px; margin: 0; line-height: 1.6;">
		<div style="max-width: 500px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; overflow: hidden; box-shadow: 0 4px 6px rgba(0,0,0,0.05);">
			
			<div style="background-color: #1e3a8a; padding: 24px; text-align: center;">
				<h1 style="margin: 0; font-size: 24px; color: #ffffff; font-weight: 700; letter-spacing: 1px;">EduPilot</h1>
			</div>

			<div style="padding: 32px;">
				<h2 style="color: #111827; font-size: 20px; margin-top: 0;">Halo, 👋</h2>
				<p style="color: #4b5563; font-size: 15px; margin-bottom: 24px;">
					Kami menerima permintaan untuk mereset kata sandi %s <strong>EduPilot</strong> Anda. Klik tombol di bawah ini untuk mengatur ulang kata sandi Anda:
				</p>

				<div style="text-align: center; margin: 32px 0;">
					<a href="%s" style="background-color: #2563eb; color: #ffffff; padding: 14px 28px; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 15px; display: inline-block;">Reset Password</a>
				</div>

				<p style="color: #6b7280; font-size: 14px;">
					Atau, Anda dapat menyalin dan menempelkan tautan berikut ke browser Anda:
					<br>
					<a href="%s" style="color: #2563eb; word-break: break-all;">%s</a>
				</p>

				<p style="color: #6b7280; font-size: 14px; margin-top: 24px;">
					Tautan ini hanya berlaku selama <strong>30 menit</strong>. Jika Anda tidak merasa membuat permintaan ini, Anda bisa mengabaikan email ini dan akun Anda akan tetap aman.
				</p>

				<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0;">

				<p style="color: #374151; font-size: 14px; margin: 0;">
					Salam hangat,<br>
					<strong>Tim EduPilot</strong>
				</p>
			</div>

			<div style="background-color: #f9fafb; border-top: 1px solid #e5e7eb; padding: 16px; text-align: center;">
				<p style="margin: 0; color: #9ca3af; font-size: 12px;">
					&copy; 2026 EduPilot. Hak Cipta Dilindungi.
				</p>
			</div>
		</div>
	</body>
	</html>
	`, account, html.EscapeString(link), html.EscapeString(link), html.EscapeString(link))
}

// BuildTeacherInviteEmail renders the Indonesian teacher-invite email. All dynamic
// values are HTML-escaped.
func BuildTeacherInviteEmail(name, schoolName, link string, ttlHours int) string {
	if strings.TrimSpace(name) == "" {
		name = "Bapak/Ibu Guru"
	}
	if strings.TrimSpace(schoolName) == "" {
		schoolName = "sekolah Anda"
	}
	escLink := html.EscapeString(link)
	return fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="id">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Undangan Akun Guru EduPilot</title>
	</head>
	<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #f3f4f6; padding: 40px 20px; margin: 0; line-height: 1.6;">
		<div style="max-width: 500px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; overflow: hidden; box-shadow: 0 4px 6px rgba(0,0,0,0.05);">

			<div style="background-color: #1e3a8a; padding: 24px; text-align: center;">
				<h1 style="margin: 0; font-size: 24px; color: #ffffff; font-weight: 700; letter-spacing: 1px;">EduPilot</h1>
			</div>

			<div style="padding: 32px;">
				<h2 style="color: #111827; font-size: 20px; margin-top: 0;">Halo, %s 👋</h2>
				<p style="color: #4b5563; font-size: 15px; margin-bottom: 24px;">
					Anda diundang oleh <strong>%s</strong> untuk bergabung di <strong>EduPilot</strong> sebagai guru. Klik tombol di bawah ini untuk mengaktifkan akun dan membuat kata sandi Anda:
				</p>

				<div style="text-align: center; margin: 32px 0;">
					<a href="%s" style="background-color: #2563eb; color: #ffffff; padding: 14px 28px; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 15px; display: inline-block;">Aktifkan Akun</a>
				</div>

				<p style="color: #6b7280; font-size: 14px;">
					Atau, Anda dapat menyalin dan menempelkan tautan berikut ke browser Anda:
					<br>
					<a href="%s" style="color: #2563eb; word-break: break-all;">%s</a>
				</p>

				<p style="color: #6b7280; font-size: 14px; margin-top: 24px;">
					Tautan ini berlaku selama <strong>%d jam</strong> dan hanya dapat digunakan satu kali. Jika Anda merasa tidak mengenal sekolah ini, abaikan saja email ini.
				</p>

				<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0;">

				<p style="color: #374151; font-size: 14px; margin: 0;">
					Salam hangat,<br>
					<strong>Tim EduPilot</strong>
				</p>
			</div>

			<div style="background-color: #f9fafb; border-top: 1px solid #e5e7eb; padding: 16px; text-align: center;">
				<p style="margin: 0; color: #9ca3af; font-size: 12px;">
					&copy; 2026 EduPilot. Hak Cipta Dilindungi.
				</p>
			</div>
		</div>
	</body>
	</html>
	`, html.EscapeString(name), html.EscapeString(schoolName), escLink, escLink, escLink, ttlHours)
}

// SendTeacherInviteEmail emails a teacher-account activation link.
func SendTeacherInviteEmail(to, name, schoolName, link string, ttlHours int) error {
	if os.Getenv("MAIL_HOST") == "" || os.Getenv("MAIL_PORT") == "" || os.Getenv("MAIL_FROM_ADDRESS") == "" {
		return fmt.Errorf("email environment variables not set properly")
	}
	subject := "Undangan Akun Guru EduPilot"
	// strip CR/LF so the school name can never inject extra headers
	if s := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(schoolName)); s != "" {
		subject = fmt.Sprintf("Undangan Akun Guru EduPilot - %s", s)
	}
	return SendEmail(to, subject, BuildTeacherInviteEmail(name, schoolName, link, ttlHours))
}

// BuildStudentInviteEmail renders the Indonesian student-invite email ("akun siswa").
// All dynamic values are HTML-escaped.
func BuildStudentInviteEmail(name, schoolName, link string, ttlHours int) string {
	if strings.TrimSpace(name) == "" {
		name = "Siswa"
	}
	if strings.TrimSpace(schoolName) == "" {
		schoolName = "sekolah Anda"
	}
	escLink := html.EscapeString(link)
	return fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="id">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Undangan Akun Siswa EduPilot</title>
	</head>
	<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #f3f4f6; padding: 40px 20px; margin: 0; line-height: 1.6;">
		<div style="max-width: 500px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; overflow: hidden; box-shadow: 0 4px 6px rgba(0,0,0,0.05);">

			<div style="background-color: #1e3a8a; padding: 24px; text-align: center;">
				<h1 style="margin: 0; font-size: 24px; color: #ffffff; font-weight: 700; letter-spacing: 1px;">EduPilot</h1>
			</div>

			<div style="padding: 32px;">
				<h2 style="color: #111827; font-size: 20px; margin-top: 0;">Halo, %s 👋</h2>
				<p style="color: #4b5563; font-size: 15px; margin-bottom: 24px;">
					<strong>%s</strong> telah membuatkan <strong>akun siswa</strong> EduPilot untuk Anda. Klik tombol di bawah ini untuk mengaktifkan akun siswa dan membuat kata sandi Anda:
				</p>

				<div style="text-align: center; margin: 32px 0;">
					<a href="%s" style="background-color: #2563eb; color: #ffffff; padding: 14px 28px; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 15px; display: inline-block;">Aktifkan Akun Siswa</a>
				</div>

				<p style="color: #6b7280; font-size: 14px;">
					Atau, Anda dapat menyalin dan menempelkan tautan berikut ke browser Anda:
					<br>
					<a href="%s" style="color: #2563eb; word-break: break-all;">%s</a>
				</p>

				<p style="color: #6b7280; font-size: 14px; margin-top: 24px;">
					Tautan ini berlaku selama <strong>%d jam</strong> dan hanya dapat digunakan satu kali. Jika Anda merasa tidak mengenal sekolah ini, abaikan saja email ini.
				</p>

				<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 32px 0;">

				<p style="color: #374151; font-size: 14px; margin: 0;">
					Salam hangat,<br>
					<strong>Tim EduPilot</strong>
				</p>
			</div>

			<div style="background-color: #f9fafb; border-top: 1px solid #e5e7eb; padding: 16px; text-align: center;">
				<p style="margin: 0; color: #9ca3af; font-size: 12px;">
					&copy; 2026 EduPilot. Hak Cipta Dilindungi.
				</p>
			</div>
		</div>
	</body>
	</html>
	`, html.EscapeString(name), html.EscapeString(schoolName), escLink, escLink, escLink, ttlHours)
}

// SendStudentInviteEmail emails a student-account activation link.
func SendStudentInviteEmail(to, name, schoolName, link string, ttlHours int) error {
	if os.Getenv("MAIL_HOST") == "" || os.Getenv("MAIL_PORT") == "" || os.Getenv("MAIL_FROM_ADDRESS") == "" {
		return fmt.Errorf("email environment variables not set properly")
	}
	subject := "Undangan Akun Siswa EduPilot"
	if s := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(schoolName)); s != "" {
		subject = fmt.Sprintf("Undangan Akun Siswa EduPilot - %s", s)
	}
	return SendEmail(to, subject, BuildStudentInviteEmail(name, schoolName, link, ttlHours))
}
