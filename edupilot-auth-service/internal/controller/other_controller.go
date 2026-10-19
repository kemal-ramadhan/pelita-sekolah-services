package controller

import (
	"net/http"
	"strconv"

	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	defaultPageLimit = 10
	maxPageLimit     = 100
)

// parsePagination applies the contract rules: page >= 1 (default 1), 1 <= limit <= 100 (default 10).
func parsePagination(c *gin.Context) (int, int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultPageLimit)))
	if err != nil || limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return page, limit
}

func paginationMeta(page, limit int, total int64) gin.H {
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return gin.H{"page": page, "limit": limit, "total": total, "total_pages": totalPages}
}

func GetStatistikCustomers(c *gin.Context) {
	result, err := service.GetCustomerStatistic()
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Customer statistic", "data": result})
}

func GetCustomersUsers(c *gin.Context) {
	page, limit := parsePagination(c)
	data, total, err := service.GetCustomerUsers(page, limit, c.Query("search"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Customer users list",
		"data":    data,
		"meta":    paginationMeta(page, limit, total),
	})
}

func GetCustomerUserDetail(c *gin.Context) {
	customerID, ok := paramUint(c, "customer_id")
	if !ok {
		return
	}
	result, err := service.GetCustomerUserDetail(customerID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Customer detail", "data": result})
}
