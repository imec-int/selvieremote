(function(angular) {
	angular.module('selvieRemote', ['websockets', 'ngAnimate'])
	.controller('checkboxController', ['$scope', 'webSockets', function($scope, webSockets) {
		$scope.ConnectedPhones = window.connectedDevices;
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
		};
		webSockets.onMessage(function(serverMessage) {
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