package sysfs

import (
    "fmt"
    "os"
    "strings"
)

const ledsBase = "/sys/class/leds"

var AllProfiles = []string{
    "blue", "white", "orange", "orange_blue", "orange_white",
    "orange_white_blue", "white_blue", "violet_blue",
    "pink", "pink_blue", "pulsate_orange",
}

func brightnessPath(profile string) string {
    return fmt.Sprintf("%s/ps4:%s:status/brightness", ledsBase, profile)
}

func SetLED(profile string) error {
    valid := profile == "off"
    for _, p := range AllProfiles {
        if p == profile {
            valid = true
            break
        }
    }
    if !valid {
        return fmt.Errorf("unknown profile %q", profile)
    }
    for _, p := range AllProfiles {
        _ = os.WriteFile(brightnessPath(p), []byte("0"), 0644)
    }
    if profile != "off" {
        return os.WriteFile(brightnessPath(profile), []byte("255"), 0644)
    }
    return nil
}

func ReadActiveProfile() string {
    for _, p := range AllProfiles {
        b, err := os.ReadFile(brightnessPath(p))
        if err == nil && strings.TrimSpace(string(b)) != "0" {
            return p
        }
    }
    return ""
}
