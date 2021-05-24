/*
Copyright 2021.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	examplev1alpha1 "github.com/DanielleChason/example-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"log"
	"net/http"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strconv"
	"strings"
)

// GitHubIssueReconciler reconciles a GitHubIssue object
type GitHubIssueReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=example.training.redhat.com,resources=githubissues,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=example.training.redhat.com,resources=githubissues/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=example.training.redhat.com,resources=githubissues/finalizers,verbs=update
// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GitHubIssue object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *GitHubIssueReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("githubissue", req.NamespacedName)
	//create new object
	ghIssue := examplev1alpha1.GitHubIssue{}

	err := r.Client.Get(ctx, req.NamespacedName, &ghIssue)

	if errors.IsNotFound(err) {
		r.Log.Info("404 - not exist")
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	//variables
	token := os.Getenv("TOKEN")
	ghDescription := ghIssue.Spec.Description
	title := ghIssue.Spec.Title
	repo := ghIssue.Spec.Repo

	//find issue
	found, issue := findIssue(title, repo, token)
	//The issue doesn't exist yet, creates new one
	if !found {
		issue = create(getApiUrl(repo), title, ghDescription, token, http.StatusCreated, "POST")
		//Update the description, if it changed
	} else if ghDescription != issue.Description {
		if issue.State == "closed" {
			r.Log.Info("cannot update a close issue")
		} else {
			issue = updateDescription(title, repo, issue.Number, ghDescription, token)
		}
		//Update status
	} else {
		issue = updateStatus(repo, issue.Number, token)
	}

	//var ghIssue *examplev1alpha1.GitHubIssue
	finalizerName := "example.training.redhat.com/finalizer"

	if ghIssue.ObjectMeta.DeletionTimestamp.IsZero() {
		if !containsString(ghIssue.GetFinalizers(), finalizerName) {
			controllerutil.AddFinalizer(&ghIssue, finalizerName)
			err := r.Update(ctx, &ghIssue)
			if err != nil {
				return ctrl.Result{}, err
			}

		}
	} else {
		if containsString(ghIssue.GetFinalizers(), finalizerName) {
			//delete the issue in github
			issue = delete(getApiUrl(repo)+"/"+strconv.Itoa(issue.Number), issue, token)

			// remove finalizer from the list and update it
			controllerutil.RemoveFinalizer(&ghIssue, finalizerName)
			err = r.Update(ctx, &ghIssue)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	patch := client.MergeFrom(ghIssue.DeepCopy())
	ghIssue.Status.State = issue.State
	ghIssue.Status.LastUpdatedTimeStamp = issue.LastUpdateTimeStamp
	err = r.Client.Status().Patch(ctx, &ghIssue, patch)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil

}

//structs
type Issue struct {
	Title               string `json:"title"`
	LastUpdateTimeStamp string `json:"updated_at"`
	Number              int    `json:"number"`
	State               string `json:"state"`
	Description         string `json:"body"`
}
type NewIssue struct {
	Title       string `json:"title"`
	Description string `json:"body"`
}

type DeletedIssue struct {
	Title string `json:"title"`
	State string `json:"state"`
}

//functions

func getApiUrl(repo string) string {
	apiURL := "https://api.github.com/repos/" + repo + "/issues"
	return apiURL
}

func getAllIssues(repo string) string {
	apiURL := "https://api.github.com/repos/" + repo + "/issues?state=all"
	return apiURL
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func findIssue(title string, repo string, token string) (bool, Issue) {
	resp := getIssues(getAllIssues(repo), repo, token)
	defer resp.Body.Close()
	var issuesList []Issue
	var issue Issue
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	json.Unmarshal(body, &issuesList)
	for _, issue := range issuesList {
		if issue.Title == title {
			return true, issue
		}
	}
	return false, issue
}

func getIssues(apiURL string, repo string, token string) *http.Response {
	details := strings.Split(getAllIssues(repo), "/")
	jsonData, _ := json.Marshal(struct {
		Repo  string
		Owner string
	}{
		Repo:  details[0],
		Owner: details[1],
	})
	client := &http.Client{}
	req, _ := http.NewRequest("GET", apiURL, bytes.NewReader(jsonData))
	req.Header.Set("Authorization", "token "+token)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	return resp
}

func connectToGitHub(apiURL string, token string, status int, method string, jsonData []byte) *http.Response {
	client := &http.Client{}
	req, _ := http.NewRequest(method, apiURL, bytes.NewReader(jsonData))
	req.Header.Set("Authorization", "token "+token)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != (status) {
		fmt.Printf(" Response code is %d\n", resp.StatusCode)
		body, _ := ioutil.ReadAll(resp.Body)
		//print body as it may contain hints in case of error
		fmt.Println(string(body))
		log.Fatal(err)
	}
	return resp
}

func create(apiURL string, title string, description string, token string, status int, method string) Issue {
	issueData := NewIssue{Title: title, Description: description}
	//make it json
	jsonData, _ := json.Marshal(issueData)
	//creating client to set custom headers for Authorization
	resp := connectToGitHub(apiURL, token, status, method, jsonData)
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	var issueCreated Issue
	json.Unmarshal(body, &issueCreated)
	return issueCreated
}

func delete(apiURL string, issue Issue, token string) Issue {
	updatedIssue := DeletedIssue{Title: issue.Title, State: "close"}
	//make it json
	jsonData, _ := json.Marshal(updatedIssue)
	//creating client to set custom headers for Authorization
	resp := connectToGitHub(apiURL, token, http.StatusOK, "PATCH", jsonData)
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	var issueUpdated Issue
	json.Unmarshal(body, &issueUpdated)
	return issueUpdated

}

func updateStatus(repo string, numIssue int, token string) Issue {
	resp := getIssues(getApiUrl(repo)+"/"+strconv.Itoa(numIssue), repo, token)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	var updatedIssue Issue
	json.Unmarshal(body, &updatedIssue)
	return updatedIssue
}

func updateDescription(issueName string, repo string, numIssue int, description string, token string) Issue {
	return create(getApiUrl(repo)+"/"+strconv.Itoa(numIssue), issueName, description, token, http.StatusOK, "PATCH")
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitHubIssueReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&examplev1alpha1.GitHubIssue{}).
		Complete(r)
}
