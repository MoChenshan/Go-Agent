package rainbow

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAppConfig_ToMap(t *testing.T) {
	Convey("Given 一个完整的AppConfig实例", t, func() {
		config := &AppConfig{
			Debug:                   true,
			OpenAIAPIKey:            "test-api-key",
			OpenAIBaseURL:           "https://api.openai.com",
			OpenAIModelName:         "gpt-4",
			BkAppCode:               "test-app-code",
			BkAppToken:              "test-app-token",
			XGatewaySecretID:        "test-secret-id",
			XGatewaySecretKey:       "test-secret-key",
			SummarizeTokenThreshold: 1000,
			MaxTraceDepth:           10,
			MaxSpanNum:              100,
			MaxSpanLogLength:        500,
			SelfTeamName:            "test-team",
			OtherTeamTruncateDepth:  3,
			KnotAPIToken:            "test-knot-token",
		}

		Convey("When 调用ToMap方法", func() {
			result := config.ToMap()

			Convey("Then 应该返回包含所有字段的map，key为toml标签名", func() {
				So(result, ShouldNotBeNil)
				So(len(result), ShouldEqual, 20) // 所有字段的数量

				// 验证各个字段的映射是否正确
				So(result["debug"], ShouldEqual, true)
				So(result["openai_api_key"], ShouldEqual, "test-api-key")
				So(result["openai_base_url"], ShouldEqual, "https://api.openai.com")
				So(result["openai_model_name"], ShouldEqual, "gpt-4")
				So(result["bk_app_code"], ShouldEqual, "test-app-code")
				So(result["bk_app_token"], ShouldEqual, "test-app-token")
				So(result["x_gateway_secret_id"], ShouldEqual, "test-secret-id")
				So(result["x_gateway_secret_key"], ShouldEqual, "test-secret-key")
				So(result["summarize_token_threshold"], ShouldEqual, 1000)
				So(result["max_trace_depth"], ShouldEqual, 10)
				So(result["max_span_num"], ShouldEqual, 100)
				So(result["max_span_log_length"], ShouldEqual, 500)
				So(result["self_team_name"], ShouldEqual, "test-team")
				So(result["other_team_truncate_depth"], ShouldEqual, 3)
				So(result["X-knot-api-token"], ShouldEqual, "test-knot-token")
			})
		})
	})

	Convey("Given 一个部分字段为空的AppConfig实例", t, func() {
		config := &AppConfig{
			Debug:                   false,
			OpenAIAPIKey:            "",
			OpenAIBaseURL:           "",
			OpenAIModelName:         "",
			BkAppCode:               "",
			BkAppToken:              "",
			XGatewaySecretID:        "",
			XGatewaySecretKey:       "",
			SummarizeTokenThreshold: 0,
			MaxTraceDepth:           0,
			MaxSpanNum:              0,
			MaxSpanLogLength:        0,
			SelfTeamName:            "",
			OtherTeamTruncateDepth:  0,
			KnotAPIToken:            "",
		}

		Convey("When 调用ToMap方法", func() {
			result := config.ToMap()

			Convey("Then 应该返回包含所有字段的map，即使值为空", func() {
				So(result, ShouldNotBeNil)
				So(len(result), ShouldEqual, 20)

				// 验证空值字段
				So(result["debug"], ShouldEqual, false)
				So(result["openai_api_key"], ShouldEqual, "")
				So(result["summarize_token_threshold"], ShouldEqual, 0)
				So(result["self_team_name"], ShouldEqual, "")
			})
		})
	})

	Convey("Given 一个nil的AppConfig指针", t, func() {
		var config *AppConfig

		Convey("When 调用ToMap方法", func() {
			Convey("Then 应该panic", func() {
				So(func() {
					_ = config.ToMap()
				}, ShouldPanic)
			})
		})
	})
}

func TestAppConfig_ToMap_FieldTypes(t *testing.T) {
	Convey("Given 一个包含不同类型字段的AppConfig实例", t, func() {
		config := &AppConfig{
			Debug:                   true,
			OpenAIAPIKey:            "string-value",
			SummarizeTokenThreshold: 123,
			MaxTraceDepth:           456,
		}

		Convey("When 调用ToMap方法", func() {
			result := config.ToMap()

			Convey("Then 应该正确保留各种数据类型", func() {
				So(result["debug"], ShouldHaveSameTypeAs, true)
				So(result["openai_api_key"], ShouldHaveSameTypeAs, "")
				So(result["summarize_token_threshold"], ShouldHaveSameTypeAs, 0)
				So(result["max_trace_depth"], ShouldHaveSameTypeAs, 0)
			})
		})
	})
}

func TestAppConfig_ToMap_TomlTagParsing(t *testing.T) {
	Convey("Given AppConfig结构体的toml标签解析逻辑", t, func() {
		config := &AppConfig{
			Debug: true,
		}

		Convey("When 调用ToMap方法", func() {
			result := config.ToMap()

			Convey("Then 应该正确处理toml标签中的选项", func() {
				// 验证标签名解析正确（没有包含omitempty等选项）
				So(result["debug"], ShouldNotBeNil)

				// 验证所有字段的key都是toml标签名
				_, debugExists := result["debug"]
				So(debugExists, ShouldBeTrue)

				_, apiKeyExists := result["openai_api_key"]
				So(apiKeyExists, ShouldBeTrue)
			})
		})
	})
}
