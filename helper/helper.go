// helper/helper.go 提供跨套件共用的小型日期格式轉換工具。
package helper

import (
	"fmt"
	"strconv"
	"strings"
)

// ROCToAD 將民國年日期字串轉成西元年日期字串。
// 輸入格式為 "YYY/MM/DD" (民國年),輸出格式為 "YYYY-MM-DD" (西元年)。
func ROCToAD(lastBuyTime string) (string, error) {
	parts := strings.Split(lastBuyTime, "/") // 將 "113/01/01" 切割成 ["113", "01", "01"]
	rocYear, err := strconv.Atoi(parts[0])   // 將 "113" 轉換成整數 113
	if err != nil {
		return "", err
	}

	adYear := rocYear + 1911               // 民國年加 1911 得西元年 (113 → 2024)
	adMonth, err := strconv.Atoi(parts[1]) // 將月份字串轉換成整數
	if err != nil {
		return "", err
	}
	adDay, err := strconv.Atoi(parts[2]) // 將日期字串轉換成整數
	if err != nil {
		return "", err
	}
	adDate := fmt.Sprintf("%04d-%02d-%02d", adYear, adMonth, adDay) // 組合成西元日期字串 "2024-01-01"
	return adDate, nil
}
