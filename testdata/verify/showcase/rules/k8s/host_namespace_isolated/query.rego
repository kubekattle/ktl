package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

knativeKinds := ["Configuration", "Service", "Revision", "ContainerSource"]
listKinds := ["Pod", "Deployment", "DaemonSet", "StatefulSet", "ReplicaSet", "ReplicationController", "Job", "CronJob"]

CxPolicy[result] {
	document := input.document[i]
	k8sLib.checkKindWithKnative(document, listKinds, knativeKinds)
	metadata := document.metadata
	specInfo := k8sLib.getSpecInfo(document)

	specInfo.spec.hostNetwork == true

	result := {
		"documentId": document.id,
		"resourceType": document.kind,
		"resourceName": metadata.name,
		"searchKey": sprintf("metadata.name={{%s}}.%s.hostNetwork", [metadata.name, specInfo.path]),
		"issueType": "IncorrectValue",
		"keyExpectedValue": sprintf("metadata.name={{%s}}.%s.hostNetwork should be false", [metadata.name, specInfo.path]),
		"keyActualValue": sprintf("metadata.name={{%s}}.%s.hostNetwork is true", [metadata.name, specInfo.path]),
		"searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["hostNetwork"]),
	}
}

CxPolicy[result] {
	document := input.document[i]
	k8sLib.checkKindWithKnative(document, listKinds, knativeKinds)
	metadata := document.metadata
	specInfo := k8sLib.getSpecInfo(document)

	specInfo.spec.hostPID == true

	result := {
		"documentId": document.id,
		"resourceType": document.kind,
		"resourceName": metadata.name,
		"searchKey": sprintf("metadata.name={{%s}}.%s.hostPID", [metadata.name, specInfo.path]),
		"issueType": "IncorrectValue",
		"keyExpectedValue": sprintf("metadata.name={{%s}}.%s.hostPID should be false", [metadata.name, specInfo.path]),
		"keyActualValue": sprintf("metadata.name={{%s}}.%s.hostPID is true", [metadata.name, specInfo.path]),
		"searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["hostPID"]),
	}
}

CxPolicy[result] {
	document := input.document[i]
	k8sLib.checkKindWithKnative(document, listKinds, knativeKinds)
	metadata := document.metadata
	specInfo := k8sLib.getSpecInfo(document)

	specInfo.spec.hostIPC == true

	result := {
		"documentId": document.id,
		"resourceType": document.kind,
		"resourceName": metadata.name,
		"searchKey": sprintf("metadata.name={{%s}}.%s.hostIPC", [metadata.name, specInfo.path]),
		"issueType": "IncorrectValue",
		"keyExpectedValue": sprintf("metadata.name={{%s}}.%s.hostIPC should be false", [metadata.name, specInfo.path]),
		"keyActualValue": sprintf("metadata.name={{%s}}.%s.hostIPC is true", [metadata.name, specInfo.path]),
		"searchLine": common_lib.build_search_line(split(specInfo.path, "."), ["hostIPC"]),
	}
}

