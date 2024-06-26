package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func GenPubKey(c *gin.Context) {
	privkey := PrivKey{}

	errors := privkey.ShouldBindJSON(c)

	if errors != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest,
			gin.H{"success": false,
				"message": "VALIDATEERR - Invalid Json",
				"errors":  strings.Split(errors.Error(), "\n")})
		return
	}

	pubkey, token, err := generateAndStorePubKey(privkey.PrivKey)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError,
			gin.H{"success": false,
				"message": "BACKENDERROR - Unable to generate pubkey & token"})
		ERROR.Printf("Unable to generate pubkey & token: %s", err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"success": true,
		"message": "PubKey and Token generated",
		"pubkey":  pubkey,
		"token":   token})

}

// privkey --> pubkey, token
// and stores it in the database
func generateAndStorePubKey(privkey string) (pubkey string, token string, err error) {

	pubkey = generatePubKey(privkey, SERVERKEY)

	if no(pubkey) {
		return "", "", fmt.Errorf("unable to generate PubKey")
	}

	token, err = generateToken()

	if err != nil {
		return "", "", err
	}

	err = txSavePubKeyAndToken(pubkey, token)

	if err != nil {
		return "", "", err
	}

	return pubkey, token, nil

}

// user#secret + SERVERKEY --> user!asdfg
func generatePubKey(privkey string, serverkey string) (pubkey string) {

	if !strings.Contains(privkey, "#") {
		return ""
	}

	username, passwd := splitPrivKey(privkey)

	if len(username) == 0 || len(passwd) == 0 {
		return ""
	}

	hmac := hmac.New(sha256.New, []byte(serverkey))

	hmac.Write([]byte(passwd))
	encrypted := hmac.Sum(nil)

	tripcode := EncodeAscii85(encrypted)

	return username + "!" + tripcode
}

// --> random string + YYYYMMDDHHMMSS (utc)
// EOL if harcoded (for now) to 99991231235959
func generateToken() (token string, err error) {

	//      YYYYMMDDHHMMSS
	TTL := "99991231235959"

	secret := make([]byte, 64)
	_, err = rand.Read(secret)
	if err != nil {
		return "", fmt.Errorf("error generating a random secret: %w", err)
	}

	return EncodeAscii85(secret) + TTL, nil
}

func GetPubKey(c *gin.Context) {
	token := Token{}

	errors := token.ShouldBindJSON(c)

	if errors != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest,
			gin.H{"success": false,
				"message": "VALIDATEERR - Invalid Json",
				"errors":  strings.Split(errors.Error(), "\n")})

		return
	}

	PubKeyAndToken := txGetByToken(token.Token)

	if no(PubKeyAndToken) {
		c.AbortWithStatusJSON(http.StatusInternalServerError,
			gin.H{"success": false,
				"message": "BACKENDERROR - Unable to obtain pubkey & token"})

		return
	}

	c.JSON(http.StatusCreated, gin.H{"success": true,
		"message": "Token found in the system",
		"pubkey":  PubKeyAndToken.Pubkey,
		"token":   PubKeyAndToken.Token})

}

// sends back the current server version + uptime
func ShowStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true,
		"version": STRINGVER,
		"uptime":  uptime(STARTEDON)})
}

// show documentation in html
func ShowDocs(c *gin.Context) {
	c.Data(http.StatusOK, gin.MIMEHTML, DOCHTML)
}

func ShowLicense(c *gin.Context) {

	LICENSE := `
    Fujined-Id, the identity management for 8bit systems<br/>
    Copyright (C) 2024 Roger Sen (roger.sen@gmail.com)<br/>
	<br/>
    This program is free software: you can redistribute it and/or modify<br/>
    it under the terms of the GNU General Public License as published by<br/>
    the Free Software Foundation, either version 3 of the License, or<br/>
    (at your option) any later version.<br/>
	<br/>
    This program is distributed in the hope that it will be useful,<br/>
    but WITHOUT ANY WARRANTY; without even the implied warranty of<br/>
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the<br/>
    GNU General Public License for more details.<br/>
	<br/>
    You should have received a copy of the GNU General Public License<br/>
    along with this program.  If not, see &lt;https://www.gnu.org/licenses/&gt;.<br/>
`

	c.Data(http.StatusOK, gin.MIMEHTML, []byte(LICENSE))
}
