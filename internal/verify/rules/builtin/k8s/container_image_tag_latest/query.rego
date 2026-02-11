package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

types := {"initContainers", "containers"}

image_tag(image) = tag {
	parts := split(image, "/")
	last := parts[count(parts) - 1]
	seg := split(last, ":")
	count(seg) > 1
	tag := seg[count(seg) - 1]
} else = tag {
	tag := ""
}

is_unpinned_tag(tag) {
	lower(tag) == "latest"
}

is_unpinned_tag(tag) {
	tag == ""
}

CxPolicy[result] {
	document := input.document[i]
	metadata := document.metadata

	specInfo := k8sLib.getSpecInfo(document)
	container := specInfo.spec[types[t]][c]

	image := container.image
	not contains(image, "@")
	tag := image_tag(image)
	is_unpinned_tag(tag)

	result := {
		"documentId": document.id,
		"resourceType": document.kind,
		"resourceName": metadata.name,
		"searchKey": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.image", [metadata.name, specInfo.path, types[t], container.name]),
		"issueType": "IncorrectValue",
		"keyExpectedValue": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.image should be pinned to a tag or digest", [metadata.name, specInfo.path, types[t], container.name]),
		"keyActualValue": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.image is %s", [metadata.name, specInfo.path, types[t], container.name, image]),
		"searchLine": common_lib.build_search_line(split(specInfo.path, "."), [types[t], c, "image"]),
	}
}
