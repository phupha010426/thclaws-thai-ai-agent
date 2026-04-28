package thaitime

import (
	"fmt"
	"time"
)

var months = []string{
	"", "ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.",
	"ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค.",
}

var fullMonths = []string{
	"", "มกราคม", "กุมภาพันธ์", "มีนาคม", "เมษายน", "พฤษภาคม", "มิถุนายน",
	"กรกฎาคม", "สิงหาคม", "กันยายน", "ตุลาคม", "พฤศจิกายน", "ธันวาคม",
}

var weekdays = []string{"อาทิตย์", "จันทร์", "อังคาร", "พุธ", "พฤหัสบดี", "ศุกร์", "เสาร์"}

func Location() *time.Location {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return time.FixedZone("Asia/Bangkok", 7*3600)
	}
	return loc
}

func Now() time.Time {
	return time.Now().In(Location())
}

func In(t time.Time) time.Time {
	return t.In(Location())
}

func ShortDate(t time.Time) string {
	t = In(t)
	return fmt.Sprintf("%d %s %02d", t.Day(), months[int(t.Month())], (t.Year()+543)%100)
}

func LongDate(t time.Time) string {
	t = In(t)
	return fmt.Sprintf("%d %s %d", t.Day(), fullMonths[int(t.Month())], t.Year()+543)
}

func Time(t time.Time) string {
	return In(t).Format("15:04")
}

func Log(t time.Time) string {
	t = In(t)
	return fmt.Sprintf("%s %s.%03d", ShortDate(t), t.Format("15:04:05"), t.Nanosecond()/1e6)
}

func ShortDateTime(t time.Time) string {
	t = In(t)
	return ShortDate(t) + " " + t.Format("15:04")
}

func TodaySentence(t time.Time) string {
	t = In(t)
	return fmt.Sprintf("วันนี้วัน%sที่ %s ครับ", weekdays[int(t.Weekday())], LongDate(t))
}
