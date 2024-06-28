package helper

import (
	"fmt"
	"strconv"
	"strings"
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
