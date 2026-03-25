package sysfs

import (
    "fmt"
    "os"
    "strconv"
    "strings"
)

const (
    hwmon        = "/sys/class/hwmon/hwmon0"
    ThresholdMin = 45
    ThresholdMax = 85
)

type FanTelemetry struct {
    ApuTempC   float64 `json:"apu_temp_c"`
    RPM        int     `json:"rpm"`
    ThresholdC int     `json:"threshold_c"`
}

func readInt(path string) (int, error) {
    b, err := os.ReadFile(path)
    if err != nil {
        return 0, err
    }
    return strconv.Atoi(strings.TrimSpace(string(b)))
}

func ReadFanTelemetry() (FanTelemetry, error) {
    temp, err := readInt(hwmon + "/temp1_input")
    if err != nil {
        return FanTelemetry{}, err
    }
    rpm, err := readInt(hwmon + "/fan1_input")
    if err != nil {
        return FanTelemetry{}, err
    }
    thresh, err := readInt(hwmon + "/temp1_crit")
    if err != nil {
        return FanTelemetry{}, err
    }
    return FanTelemetry{
        ApuTempC:   float64(temp) / 1000.0,
        RPM:        rpm,
        ThresholdC: thresh / 1000,
    }, nil
}

func WriteThreshold(c int) error {
    if c < ThresholdMin || c > ThresholdMax {
        return fmt.Errorf("threshold %d°C out of range [%d, %d]", c, ThresholdMin, ThresholdMax)
    }
    return os.WriteFile(hwmon+"/temp1_crit", []byte(strconv.Itoa(c*1000)), 0644)
}
