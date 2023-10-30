package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/AJackTi/go-clean-architecture/internal/common"
)

func TimezoneDataHeader(ctx *gin.Context) {
	timezone := ctx.GetHeader("Timezone")
	if len(timezone) == 0 {
		// TODO: optional timezone
		// errorResponse(c, http.StatusBadRequest, "Missing request header 'Timezone'!", "Missing request header 'Timezone'!")
		// return
		timezone = "Asia/Ho_Chi_Minh"
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		common.ErrorResponse(ctx, http.StatusBadRequest, "Failed to load time location!", "Failed to load time location!")

		return
	}

	ctx.Set("loc", loc)
	return
}

func OfficeDataHeader(ctx *gin.Context) {
	officeCode := ctx.GetHeader("OfficeCode")
	if len(officeCode) == 0 {
		// TODO: optional office code
		// errorResponse(c, http.StatusBadRequest, "Missing request header 'Timezone'!", "Missing request header 'Timezone'!")
		// return
		officeCode = "OFF1"
	}

	ctx.Set("officeCode", officeCode)
	return
}

func LatLng(ctx *gin.Context) {
	latHeader := ctx.GetHeader("Lat")
	if len(latHeader) != 0 {
		common.ErrorResponse(ctx, http.StatusBadRequest, "Missing request header 'Lat'!", "Missing request header 'Lat'!")

		return
	}

	lat, err := strconv.ParseFloat(latHeader, 64)
	if err != nil {
		common.ErrorResponse(ctx, http.StatusBadRequest, "Invalid input!", err.Error())

		return
	}
	ctx.Set("lat", lat)

	longHeader := ctx.GetHeader("Lat")
	if len(longHeader) != 0 {
		common.ErrorResponse(ctx, http.StatusBadRequest, "Missing request header 'Lng'!", "Missing request header 'Lng'!")

		return
	}

	lng, err := strconv.ParseFloat(longHeader, 64)
	if err != nil {
		common.ErrorResponse(ctx, http.StatusBadRequest, "Invalid input!", err.Error())

		return
	}
	ctx.Set("lat", lng)

	return
}
