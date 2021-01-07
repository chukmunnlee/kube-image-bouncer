package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/kainlite/kube-image-bouncer/rules"

	"github.com/labstack/echo"
	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var RegistryWhitelist []string

func PostValidatingAdmission() echo.HandlerFunc {
	return func(c echo.Context) error {
		var admissionReview v1beta1.AdmissionReview

		err := c.Bind(&admissionReview)
		if err != nil {
			c.Logger().Errorf("Something went wrong while unmarshalling admission review: %+v", err)
			return c.JSON(http.StatusBadRequest, err)
		}
		c.Logger().Debugf("admission review: %+v", admissionReview)

		pod := v1.Pod{}
		if err := json.Unmarshal(admissionReview.Request.Object.Raw, &pod); err != nil {
			c.Logger().Errorf("Something went wrong while unmarshalling pod object: %+v", err)
			return c.JSON(http.StatusBadRequest, err)
		}
		c.Logger().Debugf("pod: %+v", pod)

		admissionReview.Response = &v1beta1.AdmissionResponse{
			Allowed: true,
			UID:     admissionReview.Request.UID,
		}
		images := []string{}

		for _, container := range pod.Spec.Containers {
			images = append(images, container.Image)
			usingLatest, err := rules.IsUsingLatestTag(container.Image)
			if err != nil {
				c.Logger().Errorf("Error while parsing image name: %+v", err)
				return c.JSON(http.StatusInternalServerError, "error while parsing image name")
			}
			if usingLatest {
				admissionReview.Response.Allowed = false
				admissionReview.Response.Result = &metav1.Status{
					Message: "Images using latest tag are not allowed",
				}
				break
			}

			if len(RegistryWhitelist) > 0 {
				validRegistry, err := rules.IsFromWhiteListedRegistry(
					container.Image,
					RegistryWhitelist)
				if err != nil {
					c.Logger().Errorf("Error while looking for image registry: %+v", err)
					return c.JSON(
						http.StatusInternalServerError,
						"error while looking for image registry")
				}
				if !validRegistry {
					admissionReview.Response.Allowed = false
					admissionReview.Response.Result = &metav1.Status{
						Message: "Images from a non whitelisted registry",
					}
					break
				}
			}
		}

		if admissionReview.Response.Allowed {
			c.Logger().Debugf("All images accepted: %v", images)
		} else {
			c.Logger().Infof("Rejected images: %v", images)
		}

		c.Logger().Debugf("admission response: %+v", admissionReview.Response)

		return c.JSON(http.StatusOK, admissionReview)
	}
}
