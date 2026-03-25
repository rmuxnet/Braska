package auth

func VerifyToken(provided, expected string) bool {
    return expected != "" && provided == expected
}
