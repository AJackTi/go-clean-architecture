package v1

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

type Pagination struct {
	Paginate uint
	Page     uint
}

type PagingResponse struct {
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
	Total int         `json:"total"`
	Data  interface{} `json:"data"`
}

func GetPagingOption(c *gin.Context) (*Pagination, int, int) {
	pageID := c.Query("page_id")
	pageSize := c.Query("page_size")
	if pageID == "" && pageSize == "" {
		return &Pagination{
			Paginate: 25,
			Page:     1,
		}, 1, 25
	}

	page, err := strconv.Atoi(pageID)
	if err != nil || page <= 0 {
		page = 1
	}

	paginate, err := strconv.Atoi(pageSize)
	if err != nil || paginate <= 0 {
		paginate = 25
	}

	pagination := &Pagination{
		Paginate: uint(paginate),
		Page:     uint(page),
	}

	return pagination, page, paginate
}
