$(function() {
    var FADE_TIME = 150; // ms
    var TYPING_TIMER_LENGTH = 400; // ms
    var COLORS = [
        '#e21400', '#91580f', '#f8a700', '#f78b00',
        '#58dc00', '#287b00', '#a8f07a', '#4ae8c4',
        '#3b88eb', '#3824aa', '#a700ff', '#d300e7'
    ];

    var seq = 0;
    var echoCommand = 0x00;

    /**
     * create struct
     *
     * @param {object} data object
     * @param {number} default value[optional, default 0]
     * @param {boolean} endian[optional, default true]
     */
    var echoPkgHeader = new Struct ({
        Magic:'uint32',
        LogID:'uint32', // log id

        Sequence: 'uint32', // request/response sequence
        ServiceID:'uint32', // service id

        Command:'uint32', // operation command code
        Code   :'int32',  // error code

        Len:'uint16', // body length
        extra1:'uint16',
        extra2:'int32' // reserved, maybe used as package md5 checksum
    }, 0, true);

    // Initialize varibles
    var $window = $(window);
    var $messages = $('.messages'); // Messages area
    var $inputMessage = $('.inputMessage'); // Input message input box

    var $chatPage = $('.chat.page'); // The chatroom page

    // var socket = new WebSocket('ws://192.168.35.1:10000/echo');
    // var socket = new WebSocket('wss://' + serverAddress + '/echo', {
    //     // protocolVersion: 8,
    //     // origin: 'https://' + serverAddress,
    //     rejectUnauthorized: false,
    // });

    // http://stackoverflow.com/questions/7580508/getting-chrome-to-accept-self-signed-localhost-certificate#comment33762412_15076602
    // chrome://flags/#allow-insecure-localhost
    var socket = new WebSocket('wss://' + serverAddress + '/echo');
    // // Setting binaryType to accept received binary as either 'blob' or 'arraybuffer'. In default it is 'blob'.
    // socket.binaryType = 'arraybuffer';
    // socket.binaryType = ''

    function socketSend (o, silence) {
        if (socket.readyState != socket.OPEN) {
            if (!silence) {
                addChatMessage({
                    Message: '!!Connection closed'
                });
            }
            return
        }
        socket.send(JSON.stringify(o))
    }

    function marshalEchoPkg(header, msg) {
        var tmp = new Uint8Array(header.byteLength + 1 + msg.length);
        tmp.set(new Uint8Array(header), 0);

        tmp[header.byteLength] = msg.length;
        var ma = new TextEncoder("utf-8").encode(msg);
        tmp.set(ma, header.byteLength + 1);

        return tmp;
        // return tmp.buffer;
    }

    function AB2Str(buf) {
        var decoder = new TextDecoder('utf-8');
        var msg = decoder.decode(new DataView(buf));
        return msg;
    }

    function unmarshalEchoPkg(data) {
        var dataAB = new Uint8Array(data);
        var hLen = echoPkgHeader.byteLength;
        var rspHeaderData = dataAB.subarray(0, hLen);
        var rspHeader = echoPkgHeader.read(rspHeaderData.buffer);
        // console.log("echo rsp package header:" + rspHeader.Magic);
        // console.log(rspHeader.Len)
        // console.log('echo response{seq:' + rspHeader.Sequence + ', msg len:' + dataAB[hLen] + '}');
        addChatMessage({Message:
            'echo response{seq:' + rspHeader.Sequence +
            ', msg len:' + (dataAB[hLen]) +
            ', msg:' + AB2Str(new Uint8Array(dataAB.subarray(hLen + 1)).buffer) +
            '}'
            }
        );
    }

    // Sends a chat message
    function sendMessage () {
        var message = $inputMessage.val();
        // Prevent markup from being injected into the message
        message = cleanInput(message);
        // Prevent markup from being injected into the message
        // if there is a non-empty message and a socket connection
        if (message) {
            $inputMessage.val('');
            addChatMessage({
                Message: message
            });

            var pkgHeaderArrayBuffer = echoPkgHeader.write({
                Magic:0x20160905,
                LogID:Math.round(Math.random() * 2147483647),
                Command:echoCommand,
                Sequence:seq++,
                Code:0,
                Len:message.length + 1,
                extra1:0,
                extra2:0
            });
            socket.send(marshalEchoPkg(pkgHeaderArrayBuffer, message));
        }
    }

    //断开连接
    function disconnect() {
        if (socket != null) {
            socket.close();
            socket = null;
            addChatMessage({
                Message: '!!SYSTEM-WS-Close, connection disconnect'
            })
        }
    }

    // Log a message
    function log (message, options) {
        var $el = $('<li>').addClass('log').text(message);
        addMessageElement($el, options);
    }

    var imgReg = /:img\s+(\S+)/;
    // Adds the visual chat message to the message list
    function addChatMessage (data, options) {
        // Don't fade the message in if there is an 'X was typing'
        options = options || {};
        var regRes = imgReg.exec(data.Message);
        if (regRes != null) {
            var $messageBodyDiv = $('<img src="' + regRes[1] + '">');
        } else {
            var $messageBodyDiv = $('<span class="messageBody">')
                .text(data.Message);
        }

        var typingClass = data.Typing ? 'typing' : '';
        var $messageDiv = $('<li class="message"/>')
            .addClass(typingClass)
            .append($messageBodyDiv);

        addMessageElement($messageDiv, options);
    }
    // Adds a message element to the messages and scrolls to the bottom
    // el - The element to add as a message
    // options.fade - If the element should fade-in (default = true)
    // options.prepend - If the element should prepend
    //   all other messages (default = false)
    function addMessageElement (el, options) {
        // console.log("@el:" + el)
        // console.log("@options:" + options)
        var $el = $(el);

        // Setup default options
        if (!options) {
            options = {};
        }
        if (typeof options.fade === 'undefined') {
            options.fade = true;
        }
        if (typeof options.prepend === 'undefined') {
            options.prepend = false;
        }

        // Apply options
        if (options.fade) {
            $el.hide().fadeIn(FADE_TIME);
        }
        if (options.prepend) {
            $messages.prepend($el);
        } else {
            $messages.append($el);
        }
        $messages[0].scrollTop = $messages[0].scrollHeight;
    }

    // Prevents input from having injected markup
    function cleanInput (input) {
        return $('<div/>').text(input).text();
    }

    $window.keydown(function (event) {
        // When the client hits ENTER on their keyboard
        if (event.which === 13) {
            sendMessage();
        }
    });

    // Click events
    // Focus input when clicking on the message input's border
    $inputMessage.click(function () {
        $inputMessage.focus();
    });

    socket.onopen = function() {
        console.log('websocket extensions:' + socket.extensions);
    };
    // Socket events
    socket.onmessage = function(e) {
        // e.data is blob
        var fileReader = new FileReader(); // 用filereader来转blob为arraybuffer
        fileReader.onload = function() {
            var arrayBuffer = this.result; // 得到arraybuffer
            var dv = new DataView(arrayBuffer);
            unmarshalEchoPkg(dv.buffer)
        };
        fileReader.readAsArrayBuffer(e.data); // 此处读取blob
    };

    socket.onclose = function(e) {
        // console.log("socket.onclose" + e.reason)
        disconnect();
        addChatMessage({
            Message: e.reason + '!!SYSTEM-WS-Close, connection closed'
        });
    };

    socket.onerror = function() {
        addChatMessage({
            Message: '!!SYSTEM-WS-Error, connection closed'
        });
    }
});
