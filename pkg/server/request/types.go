package request

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Pagination represents apis pagination and sort params
type Pagination struct {
	// items per page
	Limit int `json:"limit" form:"limit"`
	// offset
	Offset int `json:"offset" form:"offset"`

	// pageToken
	PageToken string `json:"page_token" form:"page_token"`
	// page filed
	PageField string `json:"page_field" form:"page_field"`

	// items per page
	Size int `json:"size" form:"size"`
	// page number
	Page int `json:"page" form:"page"`

	// sort result in which field
	SortBy string `json:"sort_by" form:"sort_by"`
	// sort result in ascending or descending order, default to descending
	Ascending bool `json:"ascending" form:"ascending"`
}

func (q *Pagination) MakeSQL(db *gorm.DB) *gorm.DB {
	if q.SortBy != "" {
		db = db.Order(clause.OrderByColumn{
			Column: clause.Column{Name: q.SortBy},
			Desc:   !q.Ascending,
		})
	}

	if q.PageField != "" && q.PageToken != "" {
		symbol := "<"
		if q.Ascending {
			symbol = ">"
		}
		db = db.Where(fmt.Sprintf("? %s ?", symbol), q.PageField, q.PageToken)
	} else {
		if q.Page > 0 && q.Size > 0 {
			q.Limit = q.Size
			q.Offset = (q.Page - 1) * q.Limit
		}
		db = db.Offset(q.Offset)
	}

	if q.Limit == 0 {
		q.Limit = 10
	} else if q.Limit == -1 {
		q.Limit = 1000
	}

	return db.Limit(q.Limit)
}

// Match represents apis match params
type Match struct {
	// search keyword
	Keyword string `json:"keyword" form:"keyword"`
	// search field
	Field string `json:"field" form:"field"`
}

func (m *Match) MakeSQL(db *gorm.DB) *gorm.DB {
	if m.Field != "" && m.Keyword != "" {
		db = db.Where(fmt.Sprintf("%s LIKE ?", m.Field), "%"+m.Keyword+"%")
	}

	return db
}
