/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package common

import (
	"runtime"

	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

// LogGRPCServer logs the server-side call information via klog.
func LogGRPCServer(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	klog.V(3).Infof("GRPC call: %s", info.FullMethod)
	klog.V(5).Infof("GRPC request: %+v", protosanitizer.StripSecrets(req))
	resp, err := handler(ctx, req)
	if err != nil {
		klog.Errorf("GRPC error: %v", err)
	} else {
		klog.V(5).Infof("GRPC response: %+v", protosanitizer.StripSecrets(resp))
	}
	return resp, err
}

// LogGRPCClient does the same as LogGRPCServer, only on the client side.
func LogGRPCClient(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	klog.V(3).Infof("GRPC call: %s", method)
	klog.V(5).Infof("GRPC request: %+v", protosanitizer.StripSecrets(req))
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err != nil {
		klog.Errorf("GRPC error: %v", err)
	} else {
		klog.V(5).Infof("GRPC response: %+v", protosanitizer.StripSecrets(reply))
	}
	return err
}

// callingFunctionName returns calling function name from stack, skipping as many as wanted.
func callingFunctionName(skip int) string {
	pc := make([]uintptr, skip+1)
	n := runtime.Callers(skip, pc)
	if n == 0 {
		return "callers not found, perhaps skip was set too high?"
	}
	frames := runtime.CallersFrames(pc)
	if frames != nil {
		frame, _ := frames.Next()
		return frame.Function
	}
	return "error: nil frames"
}

// Etrace prints the calling function name trace to log with an optional prefix and postfix (1st and 2nd args).
// returns the calling function name.
func Etrace(prepost ...string) string {
	fname := callingFunctionName(3)
	traceString := fname
	if len(prepost) > 0 {
		traceString = prepost[0] + traceString
	}
	if len(prepost) > 1 {
		traceString = traceString + prepost[1]
	}
	klog.Info(traceString)
	return fname
}
