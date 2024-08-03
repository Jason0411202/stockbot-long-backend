package helper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

func ValueInStringArray(value string, array []string) int {
	for _, v := range array {
		if v == value {
			return 1
		}
	}
	return 0
}

func ROCToAD(lastBuyTime string) (string, error) {
	parts := strings.Split(lastBuyTime, "/") // 將 "113/01/01" 切割成 ["113", "01", "01"]
	rocYear, err := strconv.Atoi(parts[0])   // 將 "113" 轉換成 113
	if err != nil {
		return "", err
	}

	adYear := rocYear + 1911               // 將 113 轉換成 2024 (西元年)
	adMonth, err := strconv.Atoi(parts[1]) // Convert "01" to integer
	if err != nil {
		return "", err
	}
	adDay, err := strconv.Atoi(parts[2]) // Convert "01" to integer
	if err != nil {
		return "", err
	}
	adDate := fmt.Sprintf("%04d-%02d-%02d", adYear, adMonth, adDay) // 將 "113/01/01" 轉換成 "2024-01-01"
	return adDate, nil
}

// 生成從現在開始，往前推 1 年，每次間隔一天的日期，格式類似 "20240501"
func GenerateDates(log *logrus.Logger, days int) []string {
	now := time.Now() //取得現在時間
	Dates := make([]string, 0)
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)                    // 往前推 i 天
		Dates = append(Dates, date.Format("2006-01-02")) // 格式化為 YYYYMMDD
	}
	// 從早到晚排序
	for i, j := 0, len(Dates)-1; i < j; i, j = i+1, j-1 {
		Dates[i], Dates[j] = Dates[j], Dates[i]
	}
	log.Info("Dates: ", Dates)

	return Dates
}
