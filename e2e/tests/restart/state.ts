// Where signed-in browser state waits while deploy/ci/test.sh restarts Postbox (and changes a
// password). .state is a volume, so it outlives the test container.
export const restartState = '.state/restart.json';
export const changerState = '.state/changer.json';
export const senderState = '.state/sender.json';
