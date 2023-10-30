package common

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"time"

	"github.com/gin-gonic/gin"
)

func Distance(lat1 float64, lng1 float64, lat2 float64, lng2 float64) float64 {
	const PI float64 = 3.141592653589793

	radlat1 := PI * lat1 / 180
	radlat2 := PI * lat2 / 180

	theta := lng1 - lng2
	radtheta := PI * theta / 180

	dist := math.Sin(radlat1)*math.Sin(radlat2) + math.Cos(radlat1)*math.Cos(radlat2)*math.Cos(radtheta)

	if dist > 1 {
		dist = 1
	}

	dist = math.Acos(dist)
	dist = dist * 180 / PI
	dist = dist * 60 * 1.1515

	return dist * 1.609344
}

func IsJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

func IsWeekend(t time.Time) bool {
	t = t.UTC()
	switch t.Weekday() {
	case time.Friday:
		h, _, _ := t.Clock()
		if h >= 12+10 {
			return true
		}
	case time.Saturday:
		return true
	case time.Sunday:
		h, m, _ := t.Clock()
		if h < 12+10 {
			return true
		}
		if h == 12+10 && m <= 5 {
			return true
		}
	}
	return false
}

func DataStructToMap(obj interface{}) (newMap map[string]string, err error) {
	data, err := json.Marshal(obj) // Convert to a json string

	if err != nil {
		return
	}

	err = json.Unmarshal(data, &newMap) // Convert to a map
	return
}

func GenerateShortCode(limit int) string {
	b := make([]byte, limit)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return fmt.Sprintf("%X", b)[:limit*2-1]
}

func GetLoc(c *gin.Context) (loc *time.Location) {
	if val, ok := c.Get("loc"); ok && val != nil {
		loc, _ = val.(*time.Location)
	}

	return
}

func GetOfficeCode(c *gin.Context) (officeCode string) {
	if val, ok := c.Get("officeCode"); ok && val != nil {
		officeCode = val.(string)
	}

	return
}

func GetImageDimension(reader io.Reader) (int, int, error) {
	img, _, err := image.DecodeConfig(reader)
	if err != nil {
		log.Println("err: ", err)
		return 0, 0, err
	}

	return img.Width, img.Height, nil
}
