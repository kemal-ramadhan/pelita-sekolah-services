package service

import (
	"strings"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"gorm.io/gorm"
)

type GenderStatistic struct {
	Male    int64 `json:"male"`
	Female  int64 `json:"female"`
	Unknown int64 `json:"unknown"`
}

type AgeDistribution struct {
	Age   int `json:"age"`
	Total int `json:"total"`
}

type CustomerStatistic struct {
	TotalCustomers  int64             `json:"total_customers"`
	Gender          GenderStatistic   `json:"gender"`
	AgeDistribution []AgeDistribution `json:"age_distribution"`
}

type CustomerUserList struct {
	UserID       uint   `json:"user_id"`
	CustomerID   uint   `json:"customer_id"`
	FullName     string `json:"full_name"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	PhotoProfile string `json:"photo_profile"`
	Role         string `json:"role"`
}

type CustomerUserDetail struct {
	UserID       uint       `json:"user_id"`
	CustomerID   uint       `json:"customer_id"`
	FullName     *string    `json:"full_name"`
	BirthPlace   *string    `json:"birth_place"`
	BirthDate    *time.Time `json:"birth_date"`
	Gender       *int8      `json:"gender"`
	Email        string     `json:"email"`
	Phone        string     `json:"phone"`
	PhotoProfile string     `json:"photo_profile"`
	Role         string     `json:"role"`
	CreatedAt    time.Time  `json:"created_at"`
}

func GetCustomerStatistic() (CustomerStatistic, error) {
	var result CustomerStatistic

	if err := config.DB.Table("customers").Count(&result.TotalCustomers).Error; err != nil {
		return result, err
	}
	if err := config.DB.Table("customers").Where("gender = ?", 1).Count(&result.Gender.Male).Error; err != nil {
		return result, err
	}
	if err := config.DB.Table("customers").Where("gender = ?", 2).Count(&result.Gender.Female).Error; err != nil {
		return result, err
	}
	if err := config.DB.Table("customers").Where("gender IS NULL OR gender NOT IN (1, 2)").Count(&result.Gender.Unknown).Error; err != nil {
		return result, err
	}

	ages := []AgeDistribution{}
	if err := config.DB.
		Table("customers").
		Select(`EXTRACT(YEAR FROM AGE(birth_date)) as age, COUNT(*) as total`).
		Where("birth_date IS NOT NULL").
		Group("age").
		Order("age ASC").
		Scan(&ages).Error; err != nil {
		return result, err
	}
	result.AgeDistribution = ages
	return result, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// GetCustomerUsers returns one page of customers joined with users, plus the total count.
func GetCustomerUsers(page, limit int, search string) ([]CustomerUserList, int64, error) {
	data := []CustomerUserList{}
	var total int64

	db := config.DB.
		Table("customers").
		Joins("JOIN users ON users.id = customers.user_id")

	if search = strings.TrimSpace(search); search != "" {
		like := "%" + likeEscaper.Replace(search) + "%"
		db = db.Where(`(customers.full_name ILIKE ? OR users.email ILIKE ? OR users.phone ILIKE ?)`, like, like, like)
	}

	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := db.
		Select(`
			users.id as user_id,
			customers.id as customer_id,
			customers.full_name,
			users.email,
			users.phone,
			users.photo_profile,
			users.role
		`).
		Order("customers.created_at DESC").
		Limit(limit).
		Offset((page - 1) * limit).
		Scan(&data).Error; err != nil {
		return nil, 0, err
	}
	return data, total, nil
}

var ErrCustomerNotFound = notFound("Customer tidak ditemukan")

func GetCustomerUserDetail(customerID uint) (*CustomerUserDetail, error) {
	var result CustomerUserDetail

	res := config.DB.
		Table("customers").
		Select(`
			users.id as user_id,
			customers.id as customer_id,
			customers.full_name,
			customers.birth_place,
			customers.birth_date,
			customers.gender,
			users.email,
			users.phone,
			users.photo_profile,
			users.role,
			customers.created_at
		`).
		Joins("JOIN users ON users.id = customers.user_id").
		Where("customers.id = ?", customerID).
		Limit(1).
		Scan(&result)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrCustomerNotFound
	}
	return &result, nil
}
