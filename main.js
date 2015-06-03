(function(angular) {
	angular.module('selvieRemote', ['websockets', 'ngAnimate'])
	.controller('checkboxController', ['$scope', 'webSockets', function($scope, webSockets) {
		// value in strings instead of int since omitempty on 0 int omits it on json parsing in go
		// alternative: pointers for fields (nil pinter is for real empty element)
		$scope.intervals = [
			{name: "0s", value: "0"},
			{name: "1s", value: "1"},
			{name: "2s", value: "2"},
			{name: "5s", value: "5"},
			{name: "10s", value: "10"},
			{name: "20s", value: "20"},
			{name: "40s", value: "40"},
			{name: "1m", value: "60"},
			{name: "2m", value: "120"},
			{name: "5m", value: "300"},
		];

		$scope.globalIntervalValue = $scope.intervals[0];
		$scope.ConnectedPhones = window.connectedDevices;
		angular.forEach($scope.ConnectedPhones, function (value, key) {
			value.Interval = $scope.globalIntervalValue;
		});

		$scope.globalIntervalChanged = function () {
			angular.forEach($scope.ConnectedPhones, function (value, key) {
				value.Interval = $scope.globalIntervalValue;
			});
		}

		$scope.checkAll = function (action) {
			if(action == "record") {
				if ($scope.recordOnAll) {
					$scope.recordOnAll = true;
					// ugly shortcut to toggle all phones; set client_id to "all"
					webSockets.send({toggleRecord: "1", client_id: "all"});
				} else {
					$scope.recordOnAll = false;
					webSockets.send({toggleRecord: "0", client_id: "all"});
				}
				angular.forEach($scope.ConnectedPhones, function (value, key) {
					value.Record = $scope.recordOnAll;
				});
			}
			else if(action == "log") {
				if ($scope.postLogOnAll) {
					$scope.postLogOnAll = true;
					// ugly shortcut to toggle all phones; set client_id to "all"
					webSockets.send({postLog: "1", client_id: "all"});
				} else {
					$scope.postLogOnAll = false;
				}
				angular.forEach($scope.ConnectedPhones, function (value, key) {
					value.Log = $scope.postLogOnAll;
				});
			}
			else if(action == "wipe") {
				if ($scope.wipeOnAll) {
					$scope.wipeOnAll = true;
					// ugly shortcut to toggle all phones; set client_id to "all"
					webSockets.send({wipeVideos: "1", client_id: "all"});
				} else {
					$scope.wipeOnAll = false;
				}
				angular.forEach($scope.ConnectedPhones, function (value, key) {
					value.Wipe = $scope.wipeOnAll;
				});
			}
			else if(action == "reconnect") {
				if ($scope.reconnectOnAll) {
					$scope.reconnectOnAll = true;
				} else {
					$scope.reconnectOnAll = false;
				}
				angular.forEach($scope.ConnectedPhones, function (value, key) {
					value.Reconnect = $scope.reconnectOnAll;
					if(value.Reconnect) {
						// SEND ON WEBSOCKET
						webSockets.send({reconnectIn: $scope.ConnectedPhones[key].Interval.value, client_id: key});
					}
				});
			}
		};

		$scope.check = function (key, action) {
			if(action == "record") {
				if($scope.ConnectedPhones[key].Record) {
					webSockets.send({toggleRecord: "1", client_id: key});
				} else {
					webSockets.send({toggleRecord: "0", client_id: key});
				}
			}
			else if(action == "log") {
				if($scope.ConnectedPhones[key].Log) {
					webSockets.send({postLog: "1", client_id: key});
				} else {
					// DO NOTHING; uncheck box via websocket
				}
			}
			else if(action == "wipe") {
				if($scope.ConnectedPhones[key].Wipe) {
					webSockets.send({wipeVideos: "1", client_id: key});
				} else {
					// DO NOTHING; uncheck box via websocket
				}
			}
			else if(action == "reconnect") {
				if($scope.ConnectedPhones[key].Reconnect) {
					console.log({reconnectIn: $scope.ConnectedPhones[key].Interval.value, client_id: key});
					webSockets.send({reconnectIn: $scope.ConnectedPhones[key].Interval.value, client_id: key});
				} else {
					// DO NOTHING; phone will disappear on disconnect
				}
			}
		};

		webSockets.onMessage(function(serverMessage) {
			$scope.reconnectOnAll = false;
			console.log(serverMessage);
			var id =serverMessage.client_id;
			if(serverMessage.status) {
				if($scope.ConnectedPhones[id]) {
					$scope.ConnectedPhones[id].status = serverMessage.status;
					if(serverMessage.status == "DEL") {
						$scope.ConnectedPhones[id].Wipe = false;
					}
					else if(serverMessage.status == "LOG") {
						$scope.ConnectedPhones[id].Log = false;
					}
				}

				// uncheck global checkbox if all individuals are unchecked
				var allLogBox = false;
				var allWipeBox = false;
				angular.forEach($scope.ConnectedPhones, function (value, key) {
					allLogBox = allLogBox || value.Log;
					allWipeBox = allWipeBox || value.Wipe;
				});
				if(!allLogBox) $scope.postLogOnAll = false;
				if(!allWipeBox) $scope.wipeOnAll = false;
			}
			else if(serverMessage.isConnected === "1") {
				$scope.ConnectedPhones[id] = serverMessage;
				$scope.ConnectedPhones[id].Interval = $scope.globalIntervalValue;
			}
			else if(serverMessage.isConnected === "0") {
				delete $scope.ConnectedPhones[id];
			}
		});
	}]);



	angular.module('websockets', [])
	.factory('webSockets', function($rootScope) {
		var conn;
		var messageHandler;
		var onMessage = function(callback) {
			if(callback && typeof callback == 'function'){
				messageHandler = callback;
			}
		}
		if (window["WebSocket"]) {
			function connect() {
				conn = new WebSocket("ws://" + location.host + location.pathname);
				conn.onmessage = function(evt) {
					// force update with scope apply
					$rootScope.$apply(function() {
						if(messageHandler) {
							messageHandler(JSON.parse(evt.data));
						}
					});
				}
				conn.onclose = function(evt) {
					// reconnect on close
					setTimeout(connect, 5000);
				}
			}
			connect();
			var send = function(obj) {
				conn.send(JSON.stringify(obj));
			}
		}

		return {
			onMessage: onMessage,
			send: send
		}
	})

})(window.angular);