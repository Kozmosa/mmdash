package progress

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronField struct {
	allowed  map[int]bool
	wildcard bool
}

type parsedCron struct {
	minute     cronField
	hour       cronField
	dayOfMonth cronField
	month      cronField
	dayOfWeek  cronField
}

func parseCron(value string) (parsedCron, error) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 5 || len(value) > 100 {
		return parsedCron{}, fmt.Errorf("invalid five-field cron expression")
	}
	minute, err := parseCronField(parts[0], 0, 59, false)
	if err != nil {
		return parsedCron{}, err
	}
	hour, err := parseCronField(parts[1], 0, 23, false)
	if err != nil {
		return parsedCron{}, err
	}
	dayOfMonth, err := parseCronField(parts[2], 1, 31, false)
	if err != nil {
		return parsedCron{}, err
	}
	month, err := parseCronField(parts[3], 1, 12, false)
	if err != nil {
		return parsedCron{}, err
	}
	dayOfWeek, err := parseCronField(parts[4], 0, 7, true)
	if err != nil {
		return parsedCron{}, err
	}
	return parsedCron{
		minute: minute, hour: hour, dayOfMonth: dayOfMonth,
		month: month, dayOfWeek: dayOfWeek,
	}, nil
}

func parseCronField(value string, minimum, maximum int, sundayAlias bool) (cronField, error) {
	field := cronField{
		allowed:  map[int]bool{},
		wildcard: value == "*" || strings.HasPrefix(value, "*/"),
	}
	for _, item := range strings.Split(value, ",") {
		if item == "" {
			return cronField{}, fmt.Errorf("empty cron field item")
		}
		base, step, stepped := item, 1, false
		if strings.Contains(item, "/") {
			stepped = true
			parts := strings.Split(item, "/")
			if len(parts) != 2 {
				return cronField{}, fmt.Errorf("invalid cron step")
			}
			base = parts[0]
			parsedStep, err := strconv.Atoi(parts[1])
			if err != nil || parsedStep <= 0 {
				return cronField{}, fmt.Errorf("invalid cron step")
			}
			step = parsedStep
		}
		start, end := minimum, maximum
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			bounds := strings.Split(base, "-")
			if len(bounds) != 2 {
				return cronField{}, fmt.Errorf("invalid cron range")
			}
			var err error
			start, err = strconv.Atoi(bounds[0])
			if err != nil {
				return cronField{}, fmt.Errorf("invalid cron range")
			}
			end, err = strconv.Atoi(bounds[1])
			if err != nil {
				return cronField{}, fmt.Errorf("invalid cron range")
			}
		default:
			parsed, err := strconv.Atoi(base)
			if err != nil {
				return cronField{}, fmt.Errorf("invalid cron value")
			}
			start = parsed
			if !stepped {
				end = parsed
			}
		}
		if start < minimum || end > maximum || start > end {
			return cronField{}, fmt.Errorf("cron value out of range")
		}
		for candidate := start; candidate <= end; candidate += step {
			if sundayAlias && candidate == 7 {
				field.allowed[0] = true
			} else {
				field.allowed[candidate] = true
			}
		}
	}
	if len(field.allowed) == 0 {
		return cronField{}, fmt.Errorf("cron field has no values")
	}
	return field, nil
}

func nextCronOccurrence(value string, after time.Time) (time.Time, error) {
	schedule, err := parseCron(value)
	if err != nil {
		return time.Time{}, err
	}
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(8, 0, 0)
	for candidate.Before(limit) {
		if schedule.matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("cron expression has no occurrence in the supported window")
}

func (schedule parsedCron) matches(candidate time.Time) bool {
	if !schedule.minute.allowed[candidate.Minute()] ||
		!schedule.hour.allowed[candidate.Hour()] ||
		!schedule.month.allowed[int(candidate.Month())] {
		return false
	}
	dayOfMonth := schedule.dayOfMonth.allowed[candidate.Day()]
	dayOfWeek := schedule.dayOfWeek.allowed[int(candidate.Weekday())]
	switch {
	case schedule.dayOfMonth.wildcard && schedule.dayOfWeek.wildcard:
		return true
	case schedule.dayOfMonth.wildcard:
		return dayOfWeek
	case schedule.dayOfWeek.wildcard:
		return dayOfMonth
	default:
		return dayOfMonth || dayOfWeek
	}
}
