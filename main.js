(function(angular) {
	angular.module('selvieRemote', ['websockets'])
	.controller('checkboxController', ['$scope', 'webSockets', function($scope, webSockets) {
		$scope.ConnectedPhones = window.connectedDevices;
		$scope.checkAll = function () {
			if ($scope.selectedAll) {
				$scope.selectedAll = true;
				// ugly shortcut to toggle all phones; set client_id to "all"
				webSockets.send({toggleRecord: 1, client_id: "all"});
			} else {
				$scope.selectedAll = false;
				webSockets.send({toggleRecord: 0, client_id: "all"});
			}
			angular.forEach($scope.ConnectedPhones, function (value, key) {
				value.Selected = $scope.selectedAll;
			});
		};

		$scope.toggle = function (key) {
			if($scope.ConnectedPhones[key].Selected) {
				// 1 and 0 values are actually useless; it's a simple toggle
				// we don't keep track of the current recording status
				webSockets.send({toggleRecord: 1, client_id: key});
			} else {
				webSockets.send({toggleRecord: 0, client_id: key});
			}
		};
		webSockets.onMessage(function(serverMessage) {
			console.log(serverMessage);
			var id =serverMessage.client_id;
			if(serverMessage.status) {
				if($scope.ConnectedPhones[id]) {
					$scope.ConnectedPhones[id].status = serverMessage.status;
				}
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