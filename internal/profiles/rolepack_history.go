package profiles

// The role pack's history (ADR-0058 D1): for each seat and synced field, the SHA-256 of
// every value a released pack seeded (text normalised by normalizeField: line endings and
// trailing whitespace only), oldest first, the current value last. A stored field whose
// hash is in this list but is not the current one is STALE: still what some release
// seeded, so it can be updated safely. One that matches nothing is customised.
//
// A release that changes a seat's text adds one line per changed field here;
// TestRolePackHistory_ContainsTheCurrentText fails the build when it forgets. The
// table was generated from the git history of rolepack.go (the seats that still exist).
var roleHistory = map[string]map[string][]string{
	"peer-sonnet": {
		"instructions": {
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			"199d289a9eaa03b6470fbf8bebbb8a9c6fd15761ac8242ad46d841c886b07d30",
		},
		"notes": {
			"4420d285875fd9e6d802b528a38a01e1378a6891ba7b26d118e3031fdbb569eb",
			"9ed4e0bd3a076941febca07a4a741cfff83c607db2f9a968229cb46a18b8422a",
		},
		"skills": {
			"4e40e4b49a9bdc9c601f9aac0f11f94b25e0428e0b5dffd8015a4dad607cdbad",
		},
		"model": {
			"c13e66682022f86247ee30b2336ba8d28a498e2b2f683b85dd5290315ceb7949",
		},
		"permissionMode": {
			"929260ad9b9ea9fe0f3553dd964f4ff3deb5792efd031a2b90f573fe91f012bb",
		},
		"orchestrationTools": {
			"fcbcf165908dd18a9e49f7ff27810176db8e9f63b4352213741664245224f8aa",
		},
		"childPolicy": {
			"140bedbf9c3f6d56a9846d2ba7088798683f4da0c248231336e6a05679e4fdfe",
		},
		"onRateLimit": {
			"140bedbf9c3f6d56a9846d2ba7088798683f4da0c248231336e6a05679e4fdfe",
			"716ecabb45ac6a88a049398fde2d3d5225c6dd3121ae9bbc5af457eb4baf056a",
		},
	},
	"lead": {
		"instructions": {
			"d5184dcbebe91c3a3bb370c40ecd439da4ce398413c7b26b46a73c432ef653c5",
			"c6a0c45abb5b5ad420c99ee648bc6dcc6731d252ed6a9410451d0872d654ea63",
			"d0258694d96f11fccaa33c11a4cee6d5821477c714445b98e0e682110cba22a7",
			"88a9a6d6b7326260d2da181edae5a077624d7933ca49e63db7d62ab42a8a4b7c",
		},
		"notes": {
			"2d0684a80f80a7b41a35aff699ed2d56d348d56b9f2ed0eff9c295ef0356fe9f",
			"973c4aebb408ee9da3d6d7089828bc186c26560a35f5819c85abda0c02d18eba",
		},
		"skills": {
			"bf03324fe25969ab9b0299346f9f96ec6a9e5ed74972e5ae51839c68e3c9ec05",
		},
		"model": {
			"c13e66682022f86247ee30b2336ba8d28a498e2b2f683b85dd5290315ceb7949",
		},
		"permissionMode": {
			"929260ad9b9ea9fe0f3553dd964f4ff3deb5792efd031a2b90f573fe91f012bb",
		},
		"orchestrationTools": {
			"b5bea41b6c623f7c09f1bf24dcae58ebab3c0cdd90ad966bc43a45b44867e12b",
		},
		"childPolicy": {
			"9f91161f43433e49a6de6db680d79f60159f2e4ac9172621a12846428158440b",
		},
		"onRateLimit": {
			"140bedbf9c3f6d56a9846d2ba7088798683f4da0c248231336e6a05679e4fdfe",
			"716ecabb45ac6a88a049398fde2d3d5225c6dd3121ae9bbc5af457eb4baf056a",
		},
	},
	"validator": {
		"instructions": {
			"6a1ceb4d4a68c251ab09d81206220bc2e0a6058575fb9060693b0a4b074420a4",
		},
		"notes": {
			"a0a7f4d590be5bda445d7d8b71460ba5133ca91be25fba82e6ff8c8e615cc180",
		},
		"skills": {
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			"ea3125b040ad38ed5b7d6956d67deb06d32501acb94b8300e2b10e45866cc050",
		},
		"model": {
			"c13e66682022f86247ee30b2336ba8d28a498e2b2f683b85dd5290315ceb7949",
		},
		"permissionMode": {
			"929260ad9b9ea9fe0f3553dd964f4ff3deb5792efd031a2b90f573fe91f012bb",
		},
		"orchestrationTools": {
			"fcbcf165908dd18a9e49f7ff27810176db8e9f63b4352213741664245224f8aa",
		},
		"childPolicy": {
			"140bedbf9c3f6d56a9846d2ba7088798683f4da0c248231336e6a05679e4fdfe",
		},
		"onRateLimit": {
			"716ecabb45ac6a88a049398fde2d3d5225c6dd3121ae9bbc5af457eb4baf056a",
		},
	},
	"steward": {
		"instructions": {
			"1283b688a5e52a59462b8104cdf6a95cc010b780d1b836301ce266f2300a4ab1",
		},
		"notes": {
			"1e2283c114bbe2dd8d098e78b56ba59265dcfc9987c0cc67f393e4f2f8f9d36e",
		},
		"skills": {
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		"model": {
			"c13e66682022f86247ee30b2336ba8d28a498e2b2f683b85dd5290315ceb7949",
		},
		"permissionMode": {
			"37a8eec1ce19687d132fe29051dca629d164e2c4958ba141d5f4133a33f0688f",
		},
		"orchestrationTools": {
			"fcbcf165908dd18a9e49f7ff27810176db8e9f63b4352213741664245224f8aa",
		},
		"childPolicy": {
			"140bedbf9c3f6d56a9846d2ba7088798683f4da0c248231336e6a05679e4fdfe",
		},
		"onRateLimit": {
			"716ecabb45ac6a88a049398fde2d3d5225c6dd3121ae9bbc5af457eb4baf056a",
		},
	},
	"steward-standards": {
		"instructions": {
			"3390064369e1511a87bcd9374b0b973b45fff07e8d655195575c8b32bbcf3f67",
		},
		"notes": {
			"435b221516ddca5580e69cc9bace8540d77dce478ede1b8732df2e86ed75e4bd",
		},
		"skills": {
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		"model": {
			"26f6cf65acbd60862113d9e31ae957c4adf5fb966ae79f33a6fa8ac45e228e41",
		},
		"permissionMode": {
			"37a8eec1ce19687d132fe29051dca629d164e2c4958ba141d5f4133a33f0688f",
		},
		"orchestrationTools": {
			"fcbcf165908dd18a9e49f7ff27810176db8e9f63b4352213741664245224f8aa",
		},
		"childPolicy": {
			"140bedbf9c3f6d56a9846d2ba7088798683f4da0c248231336e6a05679e4fdfe",
		},
		"onRateLimit": {
			"716ecabb45ac6a88a049398fde2d3d5225c6dd3121ae9bbc5af457eb4baf056a",
		},
	},
}
