package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoHidesUpstreamFailureDetails(t *testing.T) {
	task := &model.Task{
		ChannelId:  84,
		Status:     model.TaskStatusFailure,
		FailReason: "All available accounts exhausted on node-49 (request id: upstream-id)",
		Data:       []byte(`{"error":{"message":"private provider failure"}}`),
		Properties: model.Properties{UpstreamModelName: "private-upstream-model"},
	}

	result := TaskModel2Dto(task)

	require.Equal(t, "Service temporarily unavailable", result.FailReason)
	require.Empty(t, result.ResultURL)
	require.Empty(t, result.Data)
	require.Nil(t, result.Properties)
	require.Zero(t, result.ChannelId)
}

func TestTaskModel2DtoHidesRoutingDetailsFromUser(t *testing.T) {
	task := &model.Task{
		ChannelId: 84,
		Properties: model.Properties{
			Input:             "public input",
			OriginModelName:   "requested-model",
			UpstreamModelName: "private-mapped-model",
		},
	}

	publicResult := TaskModel2Dto(task)
	adminResult := TaskModel2AdminDto(task)

	require.Zero(t, publicResult.ChannelId)
	require.Equal(t, model.Properties{Input: "public input", OriginModelName: "requested-model"}, publicResult.Properties)
	require.Equal(t, 84, adminResult.ChannelId)
	require.Equal(t, task.Properties, adminResult.Properties)
}
