/*
Copyright 2019 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package common

import (
	"fmt"
	"io/ioutil"
	"os"
)

// ExitError writes error to the file which path is stored
// in the TERMINATION_LOG_PATH environment variable
func ExitError(msg string, e error) {
	str := msg + ": " + e.Error()
	fmt.Println(str)
	terminationMsgPath := os.Getenv("TERMINATION_LOG_PATH")
	if terminationMsgPath != "" {
		err := ioutil.WriteFile(terminationMsgPath, []byte(str), os.FileMode(0644))
		if err != nil {
			fmt.Println("Can not create termination log file:" + terminationMsgPath)
		}
	}
}
