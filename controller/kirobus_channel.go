package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetKiroBusChannelMetadata(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel unavailable"})
		return
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil || channel == nil || !channelHasGroup(channel, c.GetString("group")) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"id":     channel.Id,
			"name":   channel.Name,
			"status": channel.Status,
			"group":  channel.Group,
		},
	})
}

func channelHasGroup(channel *model.Channel, expected string) bool {
	if channel == nil || expected == "" {
		return false
	}
	for _, group := range channel.GetGroups() {
		if group == expected {
			return true
		}
	}
	return false
}
