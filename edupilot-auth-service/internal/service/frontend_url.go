package service

import (
	"os"
	"strings"

	"github.com/edupilot-auth-service/internal/model"
)

const defaultStudentFrontendURL = "http://localhost:5175"

// customerRoleOf returns customers.role of a loaded user ("" when there is no customer row).
func customerRoleOf(u *model.User) string {
	if u == nil || u.Customer.ID == 0 {
		return ""
	}
	return u.Customer.Role
}

// isStudentAccount reports whether a customers.role value denotes a student account.
func isStudentAccount(customerRole string) bool {
	return customerRole == CustomerRoleStudents
}

// frontendBaseURL picks the app that email links point to: STUDENT_FRONTEND_URL
// (default http://localhost:5175) for student accounts, else FRONTEND_URL.
func frontendBaseURL(customerRole string) string {
	if isStudentAccount(customerRole) {
		if v := strings.TrimSpace(os.Getenv("STUDENT_FRONTEND_URL")); v != "" {
			return strings.TrimRight(v, "/")
		}
		return defaultStudentFrontendURL
	}
	return strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
}
