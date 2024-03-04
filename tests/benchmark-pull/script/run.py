from selenium import webdriver
from selenium.webdriver.chrome.options import Options
import os
import sys
import time

url = "http://testpage?" + sys.argv[1]
print(url)

options = Options();
options.add_argument("--disable-web-security")
options.add_argument("--enable-features=SharedArrayBuffer")
options.set_capability('goog:loggingPrefs', { 'browser':'ALL' });
driver = webdriver.Remote(
    command_executor = os.environ["SELENIUM_URL"],
    options = options
)
driver.implicitly_wait(20)

driver.get(url)
driver.set_script_timeout(120)
driver.execute_async_script("""
var callback = arguments[arguments.length-1];
var _console_log = console.log;
console.log = function(message) {
  _console_log.apply(console, [message]);
  if(typeof message === 'string' || message instanceof String) {
    if(message.includes("found wanted string")) {
      _console_log.apply(console, ["FOUND!"]);
      callback("ok");
    }
  }
};
""")
# time.sleep(30)

for entry in driver.get_log('browser'):
    print(entry)

driver.quit()
